package task

import (
	"context"
	stderrors "errors"
	"github.com/yuisofull/goload/internal/errors"
	"time"
)

type Service interface {
	CreateTask(ctx context.Context, param *CreateTaskParam) (*Task, error)
	GetTask(ctx context.Context, id uint64) (*Task, error)
	ListTasks(ctx context.Context, param *ListTasksParam) (*ListTasksOutput, error)
	DeleteTask(ctx context.Context, id uint64) error

	StartTask(ctx context.Context, taskID uint64) error
	PauseTask(ctx context.Context, taskID uint64) error
	ResumeTask(ctx context.Context, taskID uint64) error
	CancelTask(ctx context.Context, taskID uint64) error
	RetryTask(ctx context.Context, taskID uint64) error

	// Task updates (called by workers)
	UpdateTaskStatus(ctx context.Context, id uint64, status TaskStatus) error
	UpdateTaskProgress(ctx context.Context, id uint64, progress DownloadProgress) error
	UpdateTaskError(ctx context.Context, id uint64, err error) error
	CompleteTask(ctx context.Context, id uint64, fileInfo *FileInfo) error

	// File info and streaming
	GetFileInfo(ctx context.Context, taskID uint64) (*FileInfo, error)
	CheckFileExists(ctx context.Context, taskID uint64) (bool, error)

	// Utility
	GetTaskProgress(ctx context.Context, taskID uint64) (*DownloadProgress, error)
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

type Publisher interface {
	PublishTaskCreated(ctx context.Context, task *Task) error
}
type service struct {
	repo Repository
	pub  Publisher
	tx   TxManager
}

func NewService(repo Repository, pub Publisher, tx TxManager) Service {
	return &service{
		repo: repo,
		pub:  pub,
		tx:   tx,
	}
}

func (s *service) CreateTask(ctx context.Context, param *CreateTaskParam) (*Task, error) {
	if param.Name == "" || param.SourceURL == "" {
		return nil, &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "Name and SourceURL are required",
			Cause:   nil,
		}
	}

	task := &Task{
		Name:        param.Name,
		OfAccountID: param.OfAccountID,
		Description: param.Description,
		SourceURL:   param.SourceURL,
		SourceType:  param.SourceType,
		SourceAuth:  param.SourceAuth,
		Options:     param.Options,
		MaxRetries:  param.MaxRetries,
		Tags:        param.Tags,
		Metadata:    param.Metadata,
		Status:      StatusPending,
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

		if err := s.pub.PublishTaskCreated(ctx, createdTask); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "Failed to publish task created event",
				Cause:   err,
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return createdTask, nil
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

func (s *service) updateTask(ctx context.Context, param *UpdateTaskParam) (*TaskOutput, error) {
	task, err := s.repo.GetByID(ctx, param.TaskID)
	if err != nil {
		return nil, &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	if param.Status != "" {
		task.Status = param.Status
	}
	if param.Progress != nil {
		task.Progress = param.Progress
	}
	if param.FileInfo != nil {
		task.FileInfo = param.FileInfo
	}
	if param.Error != "" {
		task.Error = param.Error
	}

	updatedTask, err := s.repo.Update(ctx, task)
	if err != nil {
		return nil, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to update task",
			Cause:   err,
		}
	}

	return &TaskOutput{Task: updatedTask}, nil
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
	err := s.repo.Delete(ctx, id)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to delete task",
			Cause:   err,
		}
	}

	return nil
}

func (s *service) StartTask(ctx context.Context, taskID uint64) error {
	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeNotFound,
			Message: "Task not found",
			Cause:   err,
		}
	}

	if task.Status != StatusPending && task.Status != StatusPaused {
		return &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: "Task cannot be started in current status",
			Cause:   nil,
		}
	}

	task.Status = StatusDownloading
	_, err = s.repo.Update(ctx, task)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to start task",
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
	_, err = s.repo.Update(ctx, task)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to pause task",
			Cause:   err,
		}
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
	_, err = s.repo.Update(ctx, task)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to resume task",
			Cause:   err,
		}
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
	_, err = s.repo.Update(ctx, task)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to cancel task",
			Cause:   err,
		}
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
	task.RetryCount++
	_, err = s.repo.Update(ctx, task)
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "Failed to retry task",
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
	_, err = s.repo.Update(ctx, &Task{
		ID:     id,
		Error:  err.Error(),
		Status: StatusFailed,
	})
	if err != nil {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "update task error failed",
			Cause:   err,
		}
	}
	return nil
}

func (s *service) CompleteTask(ctx context.Context, id uint64, fileInfo *FileInfo) error {
	completedAt := time.Now()

	_, err := s.repo.Update(ctx, &Task{
		ID:          id,
		Status:      StatusCompleted,
		FileInfo:    fileInfo,
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

func (s *service) GetFileInfo(ctx context.Context, taskID uint64) (*FileInfo, error) {
	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		if stderrors.Is(err, errors.ErrNotFound) {
			return nil, &errors.Error{
				Code:    errors.ErrCodeNotFound,
				Message: "task not found",
				Cause:   err,
			}
		}
		return nil, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "get file info failed",
			Cause:   err,
		}
	}
	return task.FileInfo, nil
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
	return task.FileInfo != nil, nil
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
