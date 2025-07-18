package download

import (
	"context"
	"crypto/md5"
	"fmt"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/taskv2"
	"io"
	"path/filepath"
	"sync"
	"time"
)

// Downloader interface for different download sources
type Downloader interface {
	Download(ctx context.Context, url string, sourceAuth *task.AuthConfig, opts task.DownloadOptions) (io.ReadCloser, int64, error)
	GetFileInfo(ctx context.Context, url string, sourceAuth *task.AuthConfig) (*FileMetadata, error)
	SupportsResume() bool
}

// StorageBackend interface for different storage systems
type StorageBackend interface {
	Store(ctx context.Context, key string, reader io.Reader, metadata *FileMetadata) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	GetWithRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error)
	GetInfo(ctx context.Context, key string) (*task.FileInfo, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// Service handles file download operations as a worker
type Service interface {
	ExecuteTask(ctx context.Context, taskID uint64) error
	PauseTask(ctx context.Context, taskID uint64) error
	ResumeTask(ctx context.Context, taskID uint64) error
	CancelTask(ctx context.Context, taskID uint64) error
	StreamFile(ctx context.Context, req FileStreamRequest) (*FileStreamResponse, error)
	GetActiveTaskCount(ctx context.Context) int
}

// TaskServiceClient defines the contract for interacting with the task service.
type TaskServiceClient interface {
	GetTask(ctx context.Context, id uint64) (*task.Task, error)
	UpdateTaskStatus(ctx context.Context, id uint64, status task.TaskStatus) error
	UpdateTaskProgress(ctx context.Context, id uint64, progress task.DownloadProgress) error
	UpdateTaskError(ctx context.Context, id uint64, err error) error
	CompleteTask(ctx context.Context, id uint64, fileInfo *task.FileInfo) error
}

// taskExecution holds the state of a running download task.
type taskExecution struct {
	task       *task.Task
	ctx        context.Context
	cancelFunc context.CancelFunc
	progress   *task.DownloadProgress
	// The reader is now part of the execution state to allow control from other methods.
	progressReader *PausableProgressReader
}

type service struct {
	downloaders   map[task.SourceType]Downloader
	storage       StorageBackend
	taskClient    TaskServiceClient
	mu            sync.RWMutex
	activeTasks   map[uint64]*taskExecution
	maxConcurrent int
	taskTimeOut   time.Duration
	errorHandler  ErrorHandler
	sem           chan struct{}
}

type Option func(s *service)
type ErrorHandler func(ctx context.Context, err error)

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

func NewService(storage StorageBackend, taskClient task.Service, opts ...Option) Service {
	s := &service{
		downloaders:   make(map[task.SourceType]Downloader),
		storage:       storage,
		taskClient:    taskClient,
		activeTasks:   make(map[uint64]*taskExecution),
		taskTimeOut:   30 * time.Minute,
		errorHandler:  func(ctx context.Context, err error) {},
		maxConcurrent: 5,
	}

	for _, opt := range opts {
		opt(s)
	}

	s.sem = make(chan struct{}, s.maxConcurrent)

	return s
}

func (s *service) RegisterDownloader(sourceType task.SourceType, downloader Downloader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloaders[sourceType] = downloader
}

// ExecuteTask starts a download task. It is non-blocking.
func (s *service) ExecuteTask(ctx context.Context, taskID uint64) error {
	select {
	case s.sem <- struct{}{}:
	default:
		return &errors.Error{Code: errors.ErrCodeTooManyRequests, Message: "max concurrent download reached"}
	}
	defer func() { <-s.sem }()

	getTask, err := s.taskClient.GetTask(ctx, taskID)
	if err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to call get task", Cause: err}
	}

	s.mu.Lock()
	if _, ok := s.activeTasks[getTask.ID]; ok {
		s.mu.Unlock()
		return &errors.Error{Code: errors.ErrCodeConflict, Message: "task already running"}
	}

	if _, exists := s.downloaders[getTask.SourceType]; !exists {
		s.mu.Unlock()
		return &errors.Error{Code: errors.ErrCodeInvalidInput, Message: fmt.Sprintf("unsupported source type: %s", getTask.SourceType)}
	}

	taskCtx, cancel := context.WithTimeout(ctx, s.taskTimeOut)
	execution := &taskExecution{
		task:       getTask,
		ctx:        taskCtx,
		cancelFunc: cancel,
		progress:   &task.DownloadProgress{},
	}

	s.activeTasks[getTask.ID] = execution
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.activeTasks, getTask.ID)
		s.mu.Unlock()
	}()

	return s.executeDownload(execution)
}

// executeDownload performs the actual download and storage
func (s *service) executeDownload(execution *taskExecution) error {
	t := execution.task
	ctx := execution.ctx

	if err := s.taskClient.UpdateTaskStatus(ctx, t.ID, task.StatusDownloading); err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to update task status", Cause: err}
	}

	downloader := s.downloaders[t.SourceType]

	metadata, err := downloader.GetFileInfo(ctx, t.SourceURL, t.SourceAuth)
	if err != nil {
		s.markTaskFailed(ctx, t, fmt.Errorf("failed to get file info: %w", err))
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to get file info", Cause: err}
	}

	reader, totalSize, err := downloader.Download(ctx, t.SourceURL, t.SourceAuth, *t.Options)
	if err != nil {
		s.markTaskFailed(ctx, t, fmt.Errorf("failed to start download: %w", err))
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to start download", Cause: fmt.Errorf("downloading %s: %w", t.SourceURL, err)}
	}
	defer reader.Close()

	execution.progress.TotalBytes = totalSize
	s.updateProgress(ctx, t.ID, *execution.progress)

	progressReader := NewPausableProgressReader(reader, func(bytesRead int64) {
		execution.progress.BytesDownloaded = bytesRead
		if totalSize > 0 {
			execution.progress.Percentage = float64(bytesRead) / float64(totalSize) * 100
		}
		s.updateProgress(ctx, t.ID, *execution.progress)
	})

	execution.progressReader = progressReader

	if err := s.taskClient.UpdateTaskStatus(ctx, t.ID, task.StatusStoring); err != nil {
		s.errorHandler(ctx, fmt.Errorf("failed to update task status to storing: %w", err))
	}

	hash := md5.New()
	teeReader := io.TeeReader(progressReader, hash)

	storageKey := s.generateStorageKey(t)
	if err := s.storage.Store(ctx, storageKey, teeReader, metadata); err != nil {
		s.markTaskFailed(ctx, t, fmt.Errorf("failed to store file: %w", err))
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to store file", Cause: err}
	}

	md5Hash := fmt.Sprintf("%x", hash.Sum(nil))

	fileInfo := &task.FileInfo{
		FileName:    metadata.FileName,
		FileSize:    totalSize,
		ContentType: metadata.ContentType,
		MD5Hash:     md5Hash,
		StorageKey:  storageKey,
		StoredAt:    time.Now(),
	}

	if err := s.taskClient.CompleteTask(ctx, t.ID, fileInfo); err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to complete task", Cause: err}
	}

	execution.progress.BytesDownloaded = totalSize
	execution.progress.Percentage = 100.0
	s.updateProgress(ctx, t.ID, *execution.progress)
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
		return &errors.Error{Code: errors.ErrCodeConflict, Message: "task download has not started yet, cannot pause"}
	}

	execution.progressReader.Pause()

	if err := s.taskClient.UpdateTaskStatus(ctx, taskID, task.StatusPaused); err != nil {
		execution.progressReader.Resume()
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to update task status", Cause: err}
	}

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
		return &errors.Error{Code: errors.ErrCodeConflict, Message: "task is not in a state that can be resumed"}
	}

	execution.progressReader.Resume()

	if err := s.taskClient.UpdateTaskStatus(ctx, taskID, task.StatusDownloading); err != nil {
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to update task status", Cause: err}
	}

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

	return s.taskClient.UpdateTaskStatus(ctx, taskID, task.StatusCancelled)
}

// StreamFile streams a file to the client
func (s *service) StreamFile(ctx context.Context, req FileStreamRequest) (*FileStreamResponse, error) {
	getTask, err := s.taskClient.GetTask(ctx, req.TaskID)
	if err != nil {
		return nil, &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to get task", Cause: err}
	}

	if getTask.Status != task.StatusCompleted || getTask.FileInfo == nil {
		return nil, &errors.Error{Code: errors.ErrCodeNotFound, Message: "file not available for streaming"}
	}

	var reader io.ReadCloser
	var statusCode = 200

	if req.Range != nil {
		reader, err = s.storage.GetWithRange(ctx, getTask.FileInfo.StorageKey, req.Range.Start, req.Range.End)
		statusCode = 206 // Partial Content
	} else {
		reader, err = s.storage.Get(ctx, getTask.FileInfo.StorageKey)
	}

	if err != nil {
		return nil, &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to get file from storage", Cause: err}
	}

	headers := make(map[string]string)
	headers["Content-Type"] = getTask.FileInfo.ContentType
	headers["Content-Length"] = fmt.Sprintf("%d", getTask.FileInfo.FileSize)
	headers["Content-Disposition"] = fmt.Sprintf("attachment; filename=\"%s\"", getTask.FileInfo.FileName)
	headers["ETag"] = fmt.Sprintf("\"%s\"", getTask.FileInfo.MD5Hash)

	if req.Range != nil {
		headers["Content-Range"] = fmt.Sprintf("bytes %d-%d/%d", req.Range.Start, req.Range.End, getTask.FileInfo.FileSize)
	}

	return &FileStreamResponse{
		Reader:      reader,
		ContentType: getTask.FileInfo.ContentType,
		FileSize:    getTask.FileInfo.FileSize,
		FileName:    getTask.FileInfo.FileName,
		Headers:     headers,
		StatusCode:  statusCode,
	}, nil
}

func (s *service) GetActiveTaskCount(ctx context.Context) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.activeTasks)
}

// Helper methods
func (s *service) markTaskFailed(ctx context.Context, t *task.Task, err error) {
	// The context passed to UpdateTaskError should probably not be the task's context,
	// as it might be cancelled.
	bgCtx := context.Background()
	if updateErr := s.taskClient.UpdateTaskError(bgCtx, t.ID, err); updateErr != nil {
		s.errorHandler(ctx, fmt.Errorf("failed to update task error state: %w (original error: %v)", updateErr, err))
	}
}

func (s *service) updateProgress(ctx context.Context, taskID uint64, progress task.DownloadProgress) {
	if err := s.taskClient.UpdateTaskProgress(ctx, taskID, progress); err != nil {
		s.errorHandler(ctx, fmt.Errorf("failed to update task progress: %w", err))
	}
}

func (s *service) generateStorageKey(t *task.Task) string {
	return fmt.Sprintf("%d/%s", t.ID, filepath.Base(t.SourceURL))
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
		pr.mu.Lock()
		pr.totalRead += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.totalRead)
		}
		pr.mu.Unlock()
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
