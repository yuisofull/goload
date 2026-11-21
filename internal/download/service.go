package download

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
)

const DOWNLOAD_PROGRESS_UPDATE_INTERVAL = 3 * time.Second

type EventPublisher interface {
	PublishTaskStatusUpdated(ctx context.Context, event events.TaskStatusUpdatedEvent) error
	PublishTaskProgressUpdated(ctx context.Context, event events.TaskProgressUpdatedEvent) error
	PublishTaskCompleted(ctx context.Context, event events.TaskCompletedEvent) error
	PublishTaskFailed(ctx context.Context, event events.TaskFailedEvent) error
}

type Service interface {
	// ExecuteTask waits for a download slot, then starts the task asynchronously.
	// Execution failures are reported through task events and the error handler.
	ExecuteTask(ctx context.Context, req TaskRequest) error
	// PauseTask stops the queued or running execution of a task the user
	// paused. Nothing stays parked: the bytes read so far are discarded, and a
	// later resume is a fresh dispatch that downloads from zero.
	PauseTask(ctx context.Context, taskID uint64) error
	// CancelTask stops the queued or running execution of a cancelled task.
	CancelTask(ctx context.Context, taskID uint64) error
	StreamFile(ctx context.Context, req FileStreamRequest) (*FileStreamResponse, error)
	GetActiveTaskCount(ctx context.Context) int
}

// Registrar is an optional interface implemented by the concrete service that
// allows callers to register downloaders for specific source types.
type Registrar interface {
	RegisterDownloader(sourceType string, downloader Downloader)
}

type taskExecution struct {
	task       TaskRequest
	ctx        context.Context
	cancelFunc context.CancelFunc
	progress   Progress
	// stopped is set (under service.mu) when the task service paused or
	// cancelled the task. A stopped execution ends without reporting a failure.
	stopped bool
}

type service struct {
	downloaders        map[string]Downloader
	storageType        storage.Type
	storage            storage.Backend
	publisher          EventPublisher
	mu                 sync.RWMutex
	activeTasks        map[uint64]*taskExecution
	maxConcurrent      int
	taskTimeOut        time.Duration
	errorHandler       ErrorHandler
	sem                *semaphore.Weighted
	lastProgressUpdate map[uint64]time.Time
	progressMu         sync.Mutex
	// attempts, workerID and renewInterval configure the attempt-aware path
	// (RunAttempt). attempts is nil for the legacy event-driven path.
	attempts      AttemptCoordinator
	workerID      string
	renewInterval time.Duration
	// now is the clock the lease watchdog measures against. It must match the
	// clock the ownership API stamps leases with, or the watchdog misreads the
	// deadline (real clock in production, a fake clock in tests).
	now func() time.Time
}

type (
	Option       func(s *service)
	ErrorHandler func(ctx context.Context, err error)
)

func WithMaxConcurrent(maxConcurrent int) Option {
	return func(s *service) {
		s.maxConcurrent = maxConcurrent
	}
}

func WithTaskTimeout(taskTimeout time.Duration) Option {
	return func(s *service) {
		s.taskTimeOut = taskTimeout
	}
}

func WithStorageType(storageType storage.Type) Option {
	return func(s *service) {
		s.storageType = storageType
	}
}

func WithErrorHandler(errorHandler ErrorHandler) Option {
	return func(s *service) {
		s.errorHandler = errorHandler
	}
}

func NewService(storageBackend storage.Backend, publisher EventPublisher, opts ...Option) *service {
	s := &service{
		downloaders:        make(map[string]Downloader),
		storage:            storageBackend,
		publisher:          publisher,
		activeTasks:        make(map[uint64]*taskExecution),
		lastProgressUpdate: make(map[uint64]time.Time),
		taskTimeOut:        30 * time.Minute,
		errorHandler:       func(ctx context.Context, err error) {},
		maxConcurrent:      5,
		storageType:        storage.TypeLocal,
		renewInterval:      DefaultRenewInterval,
		now:                time.Now,
	}

	for _, opt := range opts {
		opt(s)
	}

	s.sem = semaphore.NewWeighted(int64(s.maxConcurrent))

	return s
}

func (s *service) RegisterDownloader(sourceType string, downloader Downloader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloaders[sourceType] = downloader
}

// ExecuteTask starts a download task and returns once it has a concurrency slot.
// The caller's context controls the lifetime of the download.
func (s *service) ExecuteTask(ctx context.Context, req TaskRequest) error {
	s.mu.Lock()
	if _, ok := s.activeTasks[req.TaskID]; ok {
		s.mu.Unlock()
		return &errors.Error{Code: errors.ErrCodeConflict, Message: "task already running"}
	}

	downloader, exists := s.downloaders[req.SourceType]
	if !exists {
		s.mu.Unlock()
		dlErr := &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: fmt.Sprintf("no downloader for source type %s", req.SourceType),
		}
		s.markTaskFailed(ctx, req.TaskID, dlErr)
		return dlErr
	}

	taskCtx, cancel := context.WithTimeout(ctx, s.taskTimeOut)
	execution := &taskExecution{
		task:       req,
		ctx:        taskCtx,
		cancelFunc: cancel,
		progress: Progress{
			Progress:        0,
			DownloadedBytes: 0,
			TotalBytes:      0,
		},
	}

	s.activeTasks[req.TaskID] = execution
	s.mu.Unlock()

	cleanup := func() {
		cancel()
		s.mu.Lock()
		delete(s.activeTasks, req.TaskID)
		s.mu.Unlock()
	}

	// Wait before spawning so a full service applies backpressure to the consumer.
	if err := s.sem.Acquire(taskCtx, 1); err != nil {
		cleanup()
		if s.isStopped(execution) {
			// Paused or cancelled while it was still queued: nothing to run
			// and nothing to redeliver.
			return nil
		}
		return fmt.Errorf("failed to acquire download slot: %w", err)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.markTaskFailed(taskCtx, req.TaskID, fmt.Errorf("recovered from panic while executing task: %v", r))
			}
			s.sem.Release(1)
			cleanup()
		}()

		if err := s.executeDownload(execution, downloader); err != nil {
			if s.isStopped(execution) {
				// Stopped by the task service (pause or cancel): the task row
				// already says so, and a failure report would overwrite it.
				return
			}
			s.markTaskFailed(taskCtx, req.TaskID, err)
		}
	}()
	return nil
}

// transferHooks are the points where the legacy event path and the attempt
// path differ while one file is downloaded and stored.
type transferHooks struct {
	// downloading runs first, before the source is contacted.
	downloading func(ctx context.Context) error
	// progress receives every progress snapshot the transfer produces.
	progress func(ctx context.Context, p Progress)
	// storing runs exactly once, when the source stream is drained and the
	// object is about to be finalized. A failure aborts the transfer.
	storing func(ctx context.Context) error
}

// transferResult describes the object one transfer stored.
type transferResult struct {
	FileName    string
	FileSize    int64
	ContentType string
	MD5         string
	StorageKey  string
}

// drainNotifier reports the end of the source stream exactly once, before
// io.EOF reaches the storage backend.
type drainNotifier struct {
	reader  io.Reader
	onDrain func() error

	mu    sync.Mutex
	fired bool
	err   error
}

func (d *drainNotifier) Read(p []byte) (int, error) {
	n, err := d.reader.Read(p)
	if err == io.EOF {
		if hookErr := d.fire(); hookErr != nil {
			return n, hookErr
		}
	}
	return n, err
}

// fire runs the hook on its first call and returns that result on every call.
func (d *drainNotifier) fire() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.fired {
		d.fired = true
		d.err = d.onDrain()
	}
	return d.err
}

// hookErr returns the hook's error; nil when it has not run or it succeeded.
func (d *drainNotifier) hookErr() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// transfer downloads the task's source and streams it into the storage key of
// this execution. It publishes nothing itself; the hooks observe it.
func (s *service) transfer(
	execution *taskExecution,
	downloader Downloader,
	hooks transferHooks,
) (*transferResult, error) {
	taskReq := execution.task
	ctx := execution.ctx

	if err := hooks.downloading(ctx); err != nil {
		return nil, err
	}

	downloadOpts := DownloadOptions{
		Concurrency: 10,
		MaxRetries:  3,
	}
	if taskReq.DownloadOptions != nil {
		downloadOpts = DownloadOptions{
			Concurrency: taskReq.DownloadOptions.Concurrency,
			MaxSpeed:    taskReq.DownloadOptions.MaxSpeed,
			MaxRetries:  taskReq.DownloadOptions.MaxRetries,
			Timeout:     taskReq.DownloadOptions.Timeout,
		}
	}
	if downloadOpts.Concurrency <= 0 {
		downloadOpts.Concurrency = 1
	}

	var sourceAuth *AuthConfig
	if taskReq.SourceAuth != nil {
		sourceAuth = &AuthConfig{
			Type:     taskReq.SourceAuth.Type,
			Username: taskReq.SourceAuth.Username,
			Password: taskReq.SourceAuth.Password,
			Token:    taskReq.SourceAuth.Token,
		}
	}

	metadata, err := downloader.GetFileInfo(ctx, taskReq.SourceURL, sourceAuth)
	if err != nil {
		return nil, &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to get file info", Cause: err}
	}

	maxRetries := max(downloadOpts.MaxRetries, 0)
	maxAttempts := maxRetries + 1
	if taskReq.DownloadOptions != nil {
		downloadOpts.MaxRetries = maxRetries
	}

	var reader io.ReadCloser
	var totalSize int64
	var dlErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reader, totalSize, dlErr = downloader.Download(ctx, taskReq.SourceURL, sourceAuth, downloadOpts)
		if dlErr == nil {
			break
		}

		// If this was the last attempt, fail
		if attempt == maxAttempts {
			return nil, &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to start download",
				Cause:   fmt.Errorf("downloading %s: %w", taskReq.SourceURL, dlErr),
			}
		}

		s.errorHandler(
			ctx,
			fmt.Errorf("downloading %s failed, retry %d/%d: %w", taskReq.SourceURL, attempt, maxRetries, dlErr),
		)
		backoff := time.Second * time.Duration(1<<attempt)
		jitter := time.Duration(time.Now().UnixNano() % int64(time.Second))
		select {
		case <-time.After(backoff + jitter):
		case <-ctx.Done():
			return nil, &errors.Error{Code: errors.ErrCodeInternal, Message: "download cancelled", Cause: ctx.Err()}
		}
	}
	// Closing the reader interrupts a source Read blocked inside Store that
	// does not itself observe ctx (FTP, BitTorrent). A dedicated watcher owns
	// the close so the transfer never waits on it: Close can itself block (an
	// FTP control-connection Quit waits for a server reply with no timeout),
	// and a transfer that waited for Close would keep holding its download
	// slot and, on the attempt path, keep its lease renewed by the heartbeat,
	// which is the exact liveness bug this guards against. The watcher closes
	// the reader exactly once, on cancellation or on normal return, and the
	// transfer returns without waiting for Close to finish. A Close that hangs
	// strands only this watcher goroutine and its own connection; it never
	// stalls dispatch or lease recovery.
	transferDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-transferDone:
		}
		_ = reader.Close()
	}()
	defer close(transferDone)

	execution.progress.TotalBytes = totalSize
	hooks.progress(ctx, execution.progress)

	progressReader := NewPausableProgressReader(reader, func(bytesRead int64) {
		p := execution.progress
		if time.Since(p.UpdatedAt) >= DOWNLOAD_PROGRESS_UPDATE_INTERVAL {
			p.DownloadedBytes = bytesRead
			if totalSize > 0 {
				p.Progress = float64(bytesRead) / float64(totalSize) * 100
			}
			p.UpdatedAt = time.Now()
			execution.progress = p
			hooks.progress(ctx, p)
		}
	})

	source := &drainNotifier{
		reader:  progressReader,
		onDrain: func() error { return hooks.storing(ctx) },
	}
	hash := md5.New()

	storageKey := s.generateStorageKey(taskReq, metadata.FileName)
	storeErr := s.storage.Store(ctx, storageKey, io.TeeReader(source, hash), &storage.FileMetadata{
		FileName:     metadata.FileName,
		FileSize:     metadata.FileSize,
		ContentType:  metadata.ContentType,
		LastModified: time.Now(),
	})
	if storeErr == nil {
		// A backend that stops reading at the announced size never reads
		// io.EOF, so make sure the storing step ran before the result is used.
		storeErr = source.fire()
	}
	if storeErr != nil {
		// storageKey belongs to this attempt alone (it carries the attempt
		// generation), so this cleanup cannot remove an object stored by
		// another attempt of the same task.
		_ = s.storage.Delete(context.Background(), storageKey)
		if hookErr := source.hookErr(); hookErr != nil {
			// The storing step itself failed (for an attempt: usually a
			// fencing rejection). Report that, not the storage error it caused.
			return nil, hookErr
		}
		return nil, &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to store file", Cause: storeErr}
	}

	return &transferResult{
		FileName:    metadata.FileName,
		FileSize:    totalSize,
		ContentType: metadata.ContentType,
		MD5:         hex.EncodeToString(hash.Sum(nil)),
		StorageKey:  storageKey,
	}, nil
}

// publishStatus publishes one status notification of the legacy path.
func (s *service) publishStatus(ctx context.Context, taskID uint64, status events.TaskStatus) error {
	if err := s.publisher.PublishTaskStatusUpdated(ctx, events.TaskStatusUpdatedEvent{
		TaskID:    taskID,
		Status:    status,
		UpdatedAt: time.Now(),
	}); err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to publish task status", Cause: err}
	}
	return nil
}

// executeDownload runs one transfer on the legacy event-driven path: status,
// progress and the result are published as task events.
func (s *service) executeDownload(execution *taskExecution, downloader Downloader) error {
	taskReq := execution.task
	ctx := execution.ctx

	result, err := s.transfer(execution, downloader, transferHooks{
		downloading: func(ctx context.Context) error {
			return s.publishStatus(ctx, taskReq.TaskID, events.StatusDownloading)
		},
		progress: func(ctx context.Context, p Progress) {
			s.updateProgress(ctx, taskReq.TaskID, p)
		},
		storing: func(ctx context.Context) error {
			return s.publishStatus(ctx, taskReq.TaskID, events.StatusStoring)
		},
	})
	if err != nil {
		return err
	}

	completedEvent := events.TaskCompletedEvent{
		TaskID:      taskReq.TaskID,
		FileName:    result.FileName,
		FileSize:    result.FileSize,
		ContentType: result.ContentType,
		Checksum: &events.ChecksumInfo{
			ChecksumType:  "md5",
			ChecksumValue: result.MD5,
		},
		StorageType: s.storageType.String(),
		StorageKey:  result.StorageKey,
		CompletedAt: time.Now(),
	}

	if err := s.publisher.PublishTaskCompleted(ctx, completedEvent); err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to publish completion event", Cause: err}
	}

	execution.progress.DownloadedBytes = result.FileSize
	execution.progress.Progress = 100.0
	execution.progress.UpdatedAt = time.Now()
	s.updateProgress(ctx, taskReq.TaskID, execution.progress)
	return nil
}

// PauseTask stops the execution of a task the user paused. See Service.
func (s *service) PauseTask(ctx context.Context, taskID uint64) error {
	return s.stopTask(taskID)
}

// CancelTask stops the execution of a cancelled task.
func (s *service) CancelTask(ctx context.Context, taskID uint64) error {
	return s.stopTask(taskID)
}

// stopTask marks a queued or running execution as stopped and cancels its
// context. The execution unwinds on its own: it frees its slot, removes its
// partial object and reports no failure.
func (s *service) stopTask(taskID uint64) error {
	s.mu.Lock()
	execution, exists := s.activeTasks[taskID]
	if exists {
		execution.stopped = true
	}
	s.mu.Unlock()

	if !exists {
		return &errors.Error{Code: errors.ErrCodeNotFound, Message: "task not found in active tasks"}
	}

	execution.cancelFunc()
	return nil
}

// isStopped reports whether the task service stopped the execution.
func (s *service) isStopped(execution *taskExecution) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return execution.stopped
}

// StreamFile streams a file to the client
func (s *service) StreamFile(ctx context.Context, req FileStreamRequest) (*FileStreamResponse, error) {
	// NOTE: This method needs to be redesigned for event-driven architecture
	// as it requires task information that is not available without direct access to task service
	return nil, &errors.Error{
		Code:    errors.ErrCodeInvalidInput,
		Message: "StreamFile requires redesign for event-driven architecture",
	}
}

func (s *service) GetActiveTaskCount(ctx context.Context) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.activeTasks)
}

func (s *service) markTaskFailed(ctx context.Context, taskID uint64, err error) {
	s.errorHandler(ctx, err)

	failEvent := events.TaskFailedEvent{
		TaskID:   taskID,
		Error:    err.Error(),
		FailedAt: time.Now(),
	}

	if publishErr := s.publisher.PublishTaskFailed(ctx, failEvent); publishErr != nil {
		s.errorHandler(
			ctx,
			fmt.Errorf("failed to publish task failed event for task %d: %w", taskID, publishErr),
		)
	}
}

func (s *service) updateProgress(ctx context.Context, taskID uint64, progress Progress) {
	s.progressMu.Lock()
	lastUpdate, ok := s.lastProgressUpdate[taskID]
	if ok && time.Since(lastUpdate) < time.Second {
		s.progressMu.Unlock()
		return
	}
	s.lastProgressUpdate[taskID] = time.Now()
	s.progressMu.Unlock()

	evt := events.TaskProgressUpdatedEvent{
		TaskID:          taskID,
		Progress:        progress.Progress,
		DownloadedBytes: progress.DownloadedBytes,
		TotalBytes:      progress.TotalBytes,
		UpdatedAt:       progress.UpdatedAt,
	}

	if err := s.publisher.PublishTaskProgressUpdated(ctx, evt); err != nil {
		s.errorHandler(ctx, fmt.Errorf("failed to publish progress for task %d: %w", taskID, err))
	}
}

func (s *service) generateStorageKey(req TaskRequest, resolvedFileName string) string {
	urlHash := md5.Sum([]byte(req.SourceURL))

	safeName := strings.TrimSpace(resolvedFileName)
	if safeName == "" {
		safeName = strings.TrimSpace(req.FileName)
	}
	if safeName == "" && !strings.HasPrefix(req.SourceURL, "data:") {
		safeName = fileNameFromSourceURL(req.SourceURL)
	}

	if safeName == "" {
		safeName = "download"
	}

	// Sanitize
	safeName = strings.ReplaceAll(safeName, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")

	if len(safeName) > 200 {
		safeName = safeName[:200]
	}

	ext := filepath.Ext(safeName)
	name := strings.TrimSuffix(safeName, ext)

	// fallback if name becomes empty (e.g. ".env")
	if name == "" {
		name = "file"
	}

	finalName := fmt.Sprintf("%s-%x%s", name, urlHash[:8], ext)

	// A claimed attempt writes under its own generation prefix, so two
	// attempts of one task never share a key and an attempt's cleanup can
	// only remove its own object. Generation 0 is the unclaimed legacy path.
	if req.Generation > 0 {
		return fmt.Sprintf("%d/%d/%s", req.TaskID, req.Generation, finalName)
	}
	return fmt.Sprintf("%d/%s", req.TaskID, finalName)
}

func fileNameFromSourceURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Path != "" {
		base := filepath.Base(parsed.Path)
		if base != "." && base != "/" {
			return base
		}
	}

	base := filepath.Base(rawURL)
	if base == "." || base == "/" {
		return ""
	}
	return base
}

// PausableProgressReader wraps a reader to support pause/resume and progress tracking.
// It is safe for concurrent use.
type PausableProgressReader struct {
	reader     io.Reader
	onProgress func(int64)
	totalRead  int64

	mu         sync.Mutex
	isPaused   bool
	resumeCond *sync.Cond
}

// NewPausableProgressReader creates a new reader that can be paused and resumed.
func NewPausableProgressReader(reader io.Reader, onProgress func(int64)) *PausableProgressReader {
	pr := &PausableProgressReader{
		reader:     reader,
		onProgress: onProgress,
	}
	pr.resumeCond = sync.NewCond(&pr.mu)
	return pr
}

// Read implements the [io.Reader] interface. It blocks if the reader is paused.
func (pr *PausableProgressReader) Read(p []byte) (n int, err error) {
	pr.mu.Lock()
	for pr.isPaused {
		pr.resumeCond.Wait()
	}
	pr.mu.Unlock()

	n, err = pr.reader.Read(p)
	if n > 0 {
		pr.mu.Lock()
		pr.totalRead += int64(n)
		total := pr.totalRead
		onProgress := pr.onProgress
		pr.mu.Unlock()
		if onProgress != nil {
			onProgress(total)
		}
	}
	return n, err
}

// Pause sets the reader to a paused state. Subsequent calls to Read will block.
func (pr *PausableProgressReader) Pause() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.isPaused = true
}

// Resume allows a paused reader to continue. It signals one waiting Read call to proceed.
func (pr *PausableProgressReader) Resume() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.isPaused = false
	pr.resumeCond.Signal()
}
