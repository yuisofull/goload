package download

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
	"golang.org/x/sync/semaphore"
)

type EventPublisher interface {
	PublishTaskStatusUpdated(ctx context.Context, event events.TaskStatusUpdatedEvent) error
	PublishTaskProgressUpdated(ctx context.Context, event events.TaskProgressUpdatedEvent) error
	PublishTaskCompleted(ctx context.Context, event events.TaskCompletedEvent) error
	PublishTaskFailed(ctx context.Context, event events.TaskFailedEvent) error
}

type Service interface {
	ExecuteTask(ctx context.Context, req TaskRequest) error
	PauseTask(ctx context.Context, taskID uint64) error
	ResumeTask(ctx context.Context, taskID uint64) error
	CancelTask(ctx context.Context, taskID uint64) error
	StreamFile(ctx context.Context, req FileStreamRequest) (*FileStreamResponse, error)
	GetActiveTaskCount(ctx context.Context) int
}

type taskExecution struct {
	task           TaskRequest
	ctx            context.Context
	cancelFunc     context.CancelFunc
	progress       Progress
	progressReader *PausableProgressReader
}

type service struct {
	downloaders        map[string]Downloader
	storage            storage.Backend
	publisher          *DownloadEventPublisher
	mu                 sync.RWMutex
	activeTasks        map[uint64]*taskExecution
	maxConcurrent      int
	taskTimeOut        time.Duration
	errorHandler       ErrorHandler
	sem                *semaphore.Weighted
	lastProgressUpdate map[uint64]time.Time
	progressMu         sync.Mutex
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

func WithErrorHandler(errorHandler ErrorHandler) Option {
	return func(s *service) {
		s.errorHandler = errorHandler
	}
}

func NewService(storage storage.Backend, publisher *DownloadEventPublisher, opts ...Option) Service {
	s := &service{
		downloaders:        make(map[string]Downloader),
		storage:            storage,
		publisher:          publisher,
		activeTasks:        make(map[uint64]*taskExecution),
		lastProgressUpdate: make(map[uint64]time.Time),
		taskTimeOut:        30 * time.Minute,
		errorHandler:       func(ctx context.Context, err error) {},
		maxConcurrent:      5,
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

// ExecuteTask starts a download task based on an internal TaskRequest
func (s *service) ExecuteTask(ctx context.Context, req TaskRequest) error {
	if err := s.sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer s.sem.Release(1)

	s.mu.Lock()
	if _, ok := s.activeTasks[req.TaskID]; ok {
		s.mu.Unlock()
		return &errors.Error{Code: errors.ErrCodeConflict, Message: "task already running"}
	}

	downloader, exists := s.downloaders[req.SourceType]
	if !exists {
		s.mu.Unlock()
		return &errors.Error{Code: errors.ErrCodeInvalidInput, Message: fmt.Sprintf("no downloader for source type %s", req.SourceType)}
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

	defer func() {
		s.mu.Lock()
		delete(s.activeTasks, req.TaskID)
		s.mu.Unlock()
	}()

	return s.executeDownload(execution, downloader)
}

// executeDownload performs the actual download and storage
func (s *service) executeDownload(execution *taskExecution, downloader Downloader) error {
	taskReq := execution.task
	ctx := execution.ctx

	if err := s.publisher.PublishTaskStatusUpdated(ctx, events.TaskStatusUpdatedEvent{
		TaskID:    taskReq.TaskID,
		Status:    events.StatusDownloading,
		UpdatedAt: time.Now(),
	}); err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to publish task status", Cause: err}
	}

	downloadOpts := DownloadOptions{
		Concurrency: 1,
		MaxRetries:  3,
	}
	if taskReq.DownloadOptions != nil {
		downloadOpts = DownloadOptions{
			Concurrency: taskReq.DownloadOptions.Concurrency,
			MaxRetries:  taskReq.DownloadOptions.MaxRetries,
		}
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
		s.markTaskFailed(ctx, taskReq.TaskID, fmt.Errorf("failed to get file info: %w", err))
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to get file info", Cause: err}
	}

	maxRetries := 3
	if taskReq.DownloadOptions != nil {
		maxRetries = int(taskReq.DownloadOptions.MaxRetries)
		if maxRetries < 0 {
			maxRetries = 0
		}
	}

	var reader io.ReadCloser
	var totalSize int64
	var dlErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		reader, totalSize, dlErr = downloader.Download(ctx, taskReq.SourceURL, sourceAuth, downloadOpts)
		if dlErr == nil {
			break
		}

		// If this was the last attempt, fail
		if attempt == maxRetries {
			s.markTaskFailed(ctx, taskReq.TaskID, fmt.Errorf("failed to start download: %w", dlErr))
			return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to start download", Cause: fmt.Errorf("downloading %s: %w", taskReq.SourceURL, dlErr)}
		}

		// _ = s.publisher.PublishTaskRetried(ctx, events.TaskRetriedEvent{
		// 	TaskID:     taskReq.TaskID,
		// 	RetryCount: uint32(attempt + 1),
		// 	Reason:     dlErr.Error(),
		// 	RetriedAt:  time.Now(),
		// })

		backoff := time.Second * time.Duration(1<<attempt)
		jitter := time.Duration(time.Now().UnixNano() % int64(time.Second))
		time.Sleep(backoff + jitter)
	}
	defer reader.Close()

	execution.progress.TotalBytes = totalSize
	s.updateProgress(ctx, taskReq.TaskID, execution.progress)

	progressReader := NewPausableProgressReader(reader, func(bytesRead int64) {
		execution.progress.DownloadedBytes = bytesRead
		if totalSize > 0 {
			execution.progress.Progress = float64(bytesRead) / float64(totalSize) * 100
		}
		execution.progress.UpdatedAt = time.Now()
		s.updateProgress(ctx, taskReq.TaskID, execution.progress)
	})

	execution.progressReader = progressReader

	if err := s.publisher.PublishTaskStatusUpdated(ctx, events.TaskStatusUpdatedEvent{
		TaskID:    taskReq.TaskID,
		Status:    events.StatusStoring,
		UpdatedAt: time.Now(),
	}); err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to publish task status", Cause: err}
	}

	hash := md5.New()
	teeReader := io.TeeReader(progressReader, hash)

	storageKey := s.generateStorageKey(taskReq)
	md5Hash := fmt.Sprintf("%x", hash.Sum(nil))
	if err := s.storage.Store(ctx, storageKey, teeReader, &storage.FileMetadata{
		FileName:     metadata.FileName,
		FileSize:     metadata.FileSize,
		ContentType:  metadata.ContentType,
		LastModified: time.Now(),
	}); err != nil {
		s.markTaskFailed(ctx, taskReq.TaskID, fmt.Errorf("failed to store file: %w", err))
		_ = s.storage.Delete(context.Background(), storageKey)
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to store file", Cause: err}
	}

	completedEvent := events.TaskCompletedEvent{
		TaskID:      taskReq.TaskID,
		FileName:    metadata.FileName,
		FileSize:    totalSize,
		ContentType: metadata.ContentType,
		Checksum: &events.ChecksumInfo{
			ChecksumType:  "md5",
			ChecksumValue: md5Hash,
		},
		StorageKey:  storageKey,
		CompletedAt: time.Now(),
	}

	if err := s.publisher.PublishTaskCompleted(ctx, completedEvent); err != nil {
		s.markTaskFailed(ctx, taskReq.TaskID, fmt.Errorf("failed to publish completion event: %w", err))
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to publish completion event", Cause: err}
	}

	execution.progress.DownloadedBytes = totalSize
	execution.progress.Progress = 100.0
	execution.progress.UpdatedAt = time.Now()
	s.updateProgress(ctx, taskReq.TaskID, execution.progress)
	return nil
}

// PauseTask pauses a running task
func (s *service) PauseTask(ctx context.Context, taskID uint64) error {
	s.mu.RLock()
	execution, exists := s.activeTasks[taskID]
	s.mu.RUnlock()

	if !exists {
		return &errors.Error{Code: errors.ErrCodeNotFound, Message: "task not found in active tasks"}
	}

	if execution.progressReader == nil {
		return &errors.Error{Code: errors.ErrCodeInvalidInput, Message: "task is not in a pausable state"}
	}

	execution.progressReader.Pause()
	return nil
}

// ResumeTask resumes a paused task
func (s *service) ResumeTask(ctx context.Context, taskID uint64) error {
	s.mu.RLock()
	execution, exists := s.activeTasks[taskID]
	s.mu.RUnlock()

	if !exists {
		return &errors.Error{Code: errors.ErrCodeNotFound, Message: "task not found in active tasks"}
	}

	if execution.progressReader == nil {
		return &errors.Error{Code: errors.ErrCodeInvalidInput, Message: "task is not in a resumable state"}
	}

	execution.progressReader.Resume()
	return nil
}

// CancelTask cancels a running task
func (s *service) CancelTask(ctx context.Context, taskID uint64) error {
	s.mu.RLock()
	execution, exists := s.activeTasks[taskID]
	s.mu.RUnlock()

	if !exists {
		return &errors.Error{Code: errors.ErrCodeNotFound, Message: "task not found in active tasks"}
	}

	execution.cancelFunc()
	return nil
}

// StreamFile streams a file to the client
func (s *service) StreamFile(ctx context.Context, req FileStreamRequest) (*FileStreamResponse, error) {
	// NOTE: This method needs to be redesigned for event-driven architecture
	// as it requires task information that is not available without direct access to task service
	return nil, &errors.Error{Code: errors.ErrCodeInvalidInput, Message: "StreamFile requires redesign for event-driven architecture"}
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

	if publishErr := s.publisher.PublishTaskFailed(context.Background(), failEvent); publishErr != nil {
		s.errorHandler(context.Background(), fmt.Errorf("failed to publish task failed event for task %d: %w", taskID, publishErr))
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

func (s *service) generateStorageKey(req TaskRequest) string {
	urlHash := md5.Sum([]byte(req.SourceURL))
	safeName := filepath.Base(req.SourceURL)
	// Sanitize the name to avoid issues with file systems
	safeName = strings.ReplaceAll(safeName, "/", "_")
	return fmt.Sprintf("%d/%s-%x", req.TaskID, safeName, urlHash[:8])
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

// Read implements the io.Reader interface. It blocks if the reader is paused.
func (pr *PausableProgressReader) Read(p []byte) (n int, err error) {
	pr.mu.Lock()
	for pr.isPaused {
		pr.resumeCond.Wait()
	}
	pr.mu.Unlock()

	n, err = pr.reader.Read(p)
	if n > 0 {
		pr.totalRead += int64(n)
		pr.onProgress(pr.totalRead)
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
