package task

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
)

type Service interface {
	CreateTask(ctx context.Context, param *CreateTaskParam) (*Task, error)
	GetTask(ctx context.Context, id uint64) (*Task, error)
	ListTasks(ctx context.Context, param *ListTasksParam) (*ListTasksOutput, error)
	DeleteTask(ctx context.Context, id uint64) error

	PauseTask(ctx context.Context, taskID uint64) error
	ResumeTask(ctx context.Context, taskID uint64) error
	CancelTask(ctx context.Context, taskID uint64) error
	RetryTask(ctx context.Context, taskID uint64) error

	// Task updates (called by workers)
	UpdateTaskStoragePath(ctx context.Context, id uint64, storagePath string) error
	UpdateTaskStatus(ctx context.Context, id uint64, status TaskStatus) error
	UpdateTaskProgress(ctx context.Context, id uint64, progress DownloadProgress) error
	UpdateTaskError(ctx context.Context, id uint64, err error) error
	CompleteTask(ctx context.Context, id uint64) error
	UpdateTaskChecksum(ctx context.Context, id uint64, checksum *ChecksumInfo) error
	UpdateTaskMetadata(ctx context.Context, id uint64, metadata map[string]any) error
	UpdateFileName(ctx context.Context, id uint64, fileName string) error
	UpdateStorageInfo(ctx context.Context, id uint64, storageType storage.Type, storagePath string) error

	// File info and streaming
	CheckFileExists(ctx context.Context, taskID uint64) (bool, error)

	// Utility
	GetTaskProgress(ctx context.Context, taskID uint64) (*DownloadProgress, error)
	// GenerateDownloadURL returns a URL clients can use to download the stored file.
	// If direct is true, the URL is a presigned storage URL. If false, the URL
	// points to a server-side download endpoint that will validate a token.
	GenerateDownloadURL(
		ctx context.Context,
		taskID uint64,
		ttl time.Duration,
		oneTime bool,
	) (url string, direct bool, err error)
}

type Repository interface {
	Create(ctx context.Context, task *Task) (*Task, error)
	GetByID(ctx context.Context, id uint64) (*Task, error)
	Update(ctx context.Context, task *Task) (*Task, error)
	ListByAccountID(ctx context.Context, filter TaskFilter, limit, offset uint32) ([]*Task, error)
	GetTaskCountOfAccount(ctx context.Context, ofAccountID uint64) (uint64, error)
	Delete(ctx context.Context, id uint64) error
}

type TxManager interface {
	DoInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type service struct {
	repo Repository
	pub  Publisher
	tx   TxManager
	// optional presigner for storage backends that support presigning
	presigner storage.Presigner
	// optional storage/presigner for pre-processing large task source payloads (e.g. uploaded .torrent files)
	taskSourceStore     storage.Writer
	taskSourcePresigner storage.Presigner
	// token store for one-time tokens fallback
	tokenStore TokenStore
	logger     log.Logger
}

const bittorrentDataURLPrefix = "data:application/x-bittorrent;base64,"

// TokenStore stores one-time or short-lived tokens for server-side download URLs.
type TokenStore interface {
	CreateToken(ctx context.Context, token string, meta storage.TokenMetadata, ttl time.Duration) error
	ConsumeToken(ctx context.Context, token string) (*storage.TokenMetadata, error)
}

// ServiceOption is a functional option for configuring the task service.
type ServiceOption func(*service)

// WithPresigner configures the task service with a storage presigner (optional).
func WithPresigner(p storage.Presigner) ServiceOption {
	return func(s *service) { s.presigner = p }
}

// WithTokenStore configures the task service with a TokenStore (optional).
func WithTokenStore(ts TokenStore) ServiceOption {
	return func(s *service) { s.tokenStore = ts }
}

// WithLogger configures the task service with a logger.
func WithLogger(l log.Logger) ServiceOption {
	return func(s *service) { s.logger = l }
}

// WithTaskSourceStore configures a storage writer used to persist large task source payloads.
func WithTaskSourceStore(w storage.Writer) ServiceOption {
	return func(s *service) { s.taskSourceStore = w }
}

// WithTaskSourcePresigner configures a presigner used to generate temporary URLs
// for stored task source payloads.
func WithTaskSourcePresigner(p storage.Presigner) ServiceOption {
	return func(s *service) { s.taskSourcePresigner = p }
}

// in-memory token store for tests or simple setups
type inmemTokenStore struct {
	mu    sync.Mutex
	store map[string]storage.TokenMetadata
}

func NewInmemTokenStore() TokenStore {
	return &inmemTokenStore{store: make(map[string]storage.TokenMetadata)}
}

func (m *inmemTokenStore) CreateToken(
	ctx context.Context,
	token string,
	meta storage.TokenMetadata,
	ttl time.Duration,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[token] = meta
	// TTL not enforced in this simple implementation
	return nil
}

func (m *inmemTokenStore) ConsumeToken(ctx context.Context, token string) (*storage.TokenMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.store[token]
	if !ok {
		return nil, nil
	}
	if meta.OneTime || time.Now().After(meta.Expires) {
		delete(m.store, token)
	}
	if time.Now().After(meta.Expires) {
		return nil, nil
	}
	return &meta, nil
}

func NewService(repo Repository, pub Publisher, tx TxManager, opts ...ServiceOption) Service {
	s := &service{
		repo:   repo,
		pub:    pub,
		tx:     tx,
		logger: log.NewNopLogger(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// GenerateDownloadURL returns a presigned URL or server token URL for the task's stored file.
func (s *service) GenerateDownloadURL(
	ctx context.Context,
	taskID uint64,
	ttl time.Duration,
	oneTime bool,
) (string, bool, error) {
	// fetch task and authorization is expected to be handled by caller
	t, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return "", false, &errors.Error{Code: errors.ErrCodeNotFound, Message: "task not found", Cause: err}
	}

	if t.StoragePath == "" {
		return "", false, &errors.Error{Code: errors.ErrCodeInvalidInput, Message: "task has no stored file"}
	}

	// Try presigner if available and oneTime == false
	if s.presigner != nil && !oneTime {
		urlStr, err := s.presigner.PresignGet(ctx, t.StoragePath, ttl)
		if err == nil {
			return urlStr, true, nil
		}
		// log and fallthrough to token path on presign error
		level.Warn(s.logger).
			Log("msg", "presign failed, falling back to token URL", "storage_path", t.StoragePath, "err", err)
	}

	if s.tokenStore == nil {
		return "", false, &errors.Error{Code: errors.ErrCodeInternal, Message: "no token store configured"}
	}

	token := uuid.New().String()
	meta := storage.TokenMetadata{
		Key:     t.StoragePath,
		OwnerID: t.OfAccountID,
		OneTime: oneTime,
		Expires: time.Now().Add(ttl),
	}

	if err := s.tokenStore.CreateToken(ctx, token, meta, ttl); err != nil {
		return "", false, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "failed to create download token",
			Cause:   err,
		}
	}

	// server endpoint is assumed to be handled by API gateway; return token URL path
	return fmt.Sprintf("/download?token=%s", url.QueryEscape(token)), false, nil
}

func (s *service) CreateTask(ctx context.Context, param *CreateTaskParam) (*Task, error) {
	if param.SourceURL == "" {
		return nil, &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "SourceURL is required",
			Cause:   nil,
		}
	}

	downloadOptions := &DownloadOptions{
		Concurrency: 16,
		MaxRetries:  3,
	}

	if strings.HasPrefix(param.SourceURL, bittorrentDataURLPrefix) {
		if s.taskSourceStore == nil || s.taskSourcePresigner == nil {
			return nil, &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "task source storage/presigner not configured",
				Cause:   nil,
			}
		}

		// Preserve BitTorrent source type even though the SourceURL will become http/https.
		if param.SourceType == "" {
			param.SourceType = SourceBitTorrent
		}

		presignedURL, err := s.storeTaskSourceTorrentDataURL(ctx, param.OfAccountID, param.FileName, param.SourceURL)
		if err != nil {
			return nil, err
		}
		param.SourceURL = presignedURL
	}

	parseUrl, err := url.Parse(param.SourceURL)
	if err != nil {
		return nil, &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "Invalid SourceURL",
			Cause:   err,
		}
	}

	if param.FileName == "" {
		param.FileName = parseUrl.Path
	}
	if param.SourceType == "" {
		param.SourceType = ToSourceType(parseUrl.Scheme)
	}

	task := &Task{
		FileName:        param.FileName,
		OfAccountID:     param.OfAccountID,
		SourceURL:       param.SourceURL,
		SourceType:      param.SourceType,
		SourceAuth:      param.SourceAuth,
		Checksum:        param.Checksum,
		DownloadOptions: downloadOptions,
		Metadata:        param.Metadata,
		Status:          StatusPending,
	}

	var createdTask *Task
	if err := s.tx.DoInTx(ctx, func(ctx context.Context) error {
		var err error
		createdTask, err = s.repo.Create(ctx, task)
		if err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "Failed to create task",
				Cause:   err,
			}
		}

		// Publish TaskCreated event inside the same transaction. If publishing fails
		// the transaction should be rolled back by returning an error here.
		if err := s.pub.PublishTaskCreated(ctx, createdTask); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to publish task created event",
				Cause:   err,
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return createdTask, nil
}

func (s *service) storeTaskSourceTorrentDataURL(
	ctx context.Context,
	ofAccountID uint64,
	fileName string,
	dataURL string,
) (string, error) {
	// Decode
	encoded := strings.TrimPrefix(dataURL, bittorrentDataURLPrefix)
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", &errors.Error{Code: errors.ErrCodeInvalidInput, Message: "empty torrent base64 payload"}
	}

	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", &errors.Error{Code: errors.ErrCodeInvalidInput, Message: "invalid torrent base64 payload", Cause: err}
	}

	// Build a stable-ish object name for observability while ensuring uniqueness.
	baseName := strings.TrimSpace(filepath.Base(fileName))
	if baseName == "" || baseName == "." || baseName == "/" {
		baseName = "source.torrent"
	}
	if !strings.HasSuffix(strings.ToLower(baseName), ".torrent") {
		baseName = baseName + ".torrent"
	}
	baseStem := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	if baseStem == "" {
		baseStem = "source"
	}

	var randBytes [16]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		return "", &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to generate object key", Cause: err}
	}
	rnd := hex.EncodeToString(randBytes[:])
	key := fmt.Sprintf("%d/%s-%s.torrent", ofAccountID, baseStem, rnd[:8])

	// Upload (best-effort expiry of 24h)
	expiresAt := time.Now().Add(24 * time.Hour)
	if err := s.taskSourceStore.Store(ctx, key, bytes.NewReader(content), &storage.FileMetadata{
		FileName:    baseName,
		ContentType: "application/x-bittorrent",
		Expiry:      expiresAt,
	}); err != nil {
		return "", &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to store torrent source", Cause: err}
	}

	// Presign (24h)
	urlStr, err := s.taskSourcePresigner.PresignGet(ctx, key, 24*time.Hour)
	if err != nil {
		return "", &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to presign torrent source", Cause: err}
	}

	return urlStr, nil
}

func (s *service) GetTask(ctx context.Context, id uint64) (*Task, error) {
	task, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	return task, nil
}

func (s *service) ListTasks(ctx context.Context, param *ListTasksParam) (*ListTasksOutput, error) {
	tasks, err := s.repo.ListByAccountID(ctx, *param.Filter, uint32(param.Limit), uint32(param.Offset))
	if err != nil {
		return nil, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to list tasks",
			Cause:   err,
		}
	}

	return &ListTasksOutput{Tasks: tasks, Total: int32(len(tasks))}, nil
}

func (s *service) DeleteTask(ctx context.Context, id uint64) error {
	// Verify the task exists before attempting deletion to avoid silent no-ops.
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	// We must tell the download service to stop the worker thread.
	// We do this by publishing a TaskCancelled event.
	// The download service listens for task.cancelled to stop running tasks.
	if err := s.pub.PublishTaskCancelled(ctx, id); err != nil {
		level.Warn(s.logger).Log("msg", "Failed to publish task cancelled event during delete", "task_id", id, "err", err)
		// We still proceed to delete the record from the DB
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to delete task",
			Cause:   err,
		}
	}

	return nil
}

func (s *service) PauseTask(ctx context.Context, taskID uint64) error {
	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	if task.Status != StatusDownloading {
		return &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "Only downloading tasks can be paused",
			Cause:   nil,
		}
	}

	task.Status = StatusPaused

	if err := s.tx.DoInTx(ctx, func(ctx context.Context) error {
		_, err := s.repo.Update(ctx, task)
		if err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "Failed to pause task",
				Cause:   err,
			}
		}

		if err := s.pub.PublishTaskPaused(ctx, taskID); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to publish task paused event",
				Cause:   err,
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (s *service) ResumeTask(ctx context.Context, taskID uint64) error {
	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	if task.Status != StatusPaused {
		return &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "Only paused tasks can be resumed",
			Cause:   nil,
		}
	}

	task.Status = StatusDownloading

	if err := s.tx.DoInTx(ctx, func(ctx context.Context) error {
		_, err := s.repo.Update(ctx, task)
		if err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "Failed to resume task",
				Cause:   err,
			}
		}

		if err := s.pub.PublishTaskResumed(ctx, taskID); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to publish task resumed event",
				Cause:   err,
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (s *service) CancelTask(ctx context.Context, taskID uint64) error {
	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	if task.Status == StatusCompleted || task.Status == StatusCancelled {
		return &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "Completed or cancelled tasks cannot be cancelled",
			Cause:   nil,
		}
	}

	task.Status = StatusCancelled

	if err := s.tx.DoInTx(ctx, func(ctx context.Context) error {
		_, err := s.repo.Update(ctx, task)
		if err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "Failed to cancel task",
				Cause:   err,
			}
		}

		if err := s.pub.PublishTaskCancelled(ctx, taskID); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to publish task cancelled event",
				Cause:   err,
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (s *service) RetryTask(ctx context.Context, taskID uint64) error {
	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	if task.Status != StatusFailed {
		return &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "Only failed tasks can be retried",
			Cause:   nil,
		}
	}

	task.Status = StatusPending

	if err := s.tx.DoInTx(ctx, func(ctx context.Context) error {
		_, err := s.repo.Update(ctx, task)
		if err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "Failed to retry task",
				Cause:   err,
			}
		}

		// Publish TaskCreated so the download worker re-picks the task.
		// PublishTaskStatusUpdated is NOT consumed by the download service;
		// only "task.created" events trigger execution.
		if err := s.pub.PublishTaskCreated(ctx, task); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to publish task created event for retry",
				Cause:   err,
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (s *service) UpdateTaskStoragePath(ctx context.Context, id uint64, storagePath string) error {
	_, err := s.repo.Update(ctx, &Task{
		ID:          id,
		StoragePath: storagePath,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task storage path failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) UpdateStorageInfo(
	ctx context.Context,
	id uint64,
	storageType storage.Type,
	storagePath string,
) error {
	_, err := s.repo.Update(ctx, &Task{
		ID:          id,
		StorageType: storageType,
		StoragePath: storagePath,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task storage info failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) UpdateFileName(ctx context.Context, id uint64, fileName string) error {
	_, err := s.repo.Update(ctx, &Task{
		ID:       id,
		FileName: fileName,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task file name failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) UpdateTaskStatus(ctx context.Context, id uint64, status TaskStatus) error {
	_, err := s.repo.Update(ctx, &Task{
		ID:     id,
		Status: status,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task status failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) UpdateTaskProgress(ctx context.Context, id uint64, progress DownloadProgress) error {
	_, err := s.repo.Update(ctx, &Task{
		ID:       id,
		Progress: &progress,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task progress failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) UpdateTaskError(ctx context.Context, id uint64, err error) error {
	_, updateErr := s.repo.Update(ctx, &Task{
		ID:     id,
		Status: StatusFailed,
	})
	if updateErr != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task error failed",
			Cause:   updateErr,
		}
	}
	return nil
}

func (s *service) CompleteTask(ctx context.Context, id uint64) error {
	completedAt := time.Now()

	_, err := s.repo.Update(ctx, &Task{
		ID:          id,
		Status:      StatusCompleted,
		CompletedAt: &completedAt,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "complete task failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) UpdateTaskChecksum(ctx context.Context, id uint64, checksum *ChecksumInfo) error {
	_, err := s.repo.Update(ctx, &Task{
		ID:       id,
		Checksum: checksum,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task checksum failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) UpdateTaskMetadata(ctx context.Context, id uint64, metadata map[string]interface{}) error {
	_, err := s.repo.Update(ctx, &Task{
		ID:       id,
		Metadata: metadata,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task metadata failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) CheckFileExists(ctx context.Context, taskID uint64) (bool, error) {
	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		if stderrors.Is(err, errors.ErrNotFound) {
			return false, &errors.Error{
				Code:    errors.ErrCodeNotFound,
				Message: "task not found",
				Cause:   err,
			}
		}
		return false, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "check file exists failed",
			Cause:   err,
		}
	}

	if task.Status != StatusCompleted {
		return false, nil
	}
	return true, nil
}

func (s *service) GetTaskProgress(ctx context.Context, id uint64) (*DownloadProgress, error) {
	task, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "get task progress failed",
			Cause:   err,
		}
	}
	return task.Progress, nil
}
