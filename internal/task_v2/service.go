package task_v2

import (
	"context"
	stderrors "errors"
	"github.com/samber/lo"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/file"
)

type CreateParams struct {
	UserID       uint64
	DownloadType file.DownloadType
	Url          string
}

type CreateResult struct {
	DownloadTask *DownloadTask
}

type GetParams struct {
	UserID         uint64
	DownloadTaskID uint64
}

type GetResult struct {
	DownloadTask *DownloadTask
}
type ListParams struct {
	UserID uint64
	Offset uint64
	Limit  uint64
}

type ListResult struct {
	DownloadTasks []*DownloadTask
	TotalCount    uint64
}

type UpdateParams struct {
	UserID         uint64
	DownloadTaskId uint64
	Url            string
}

type UpdateResult struct {
	DownloadTask *DownloadTask
}

type DeleteParams struct {
	UserID       uint64
	DownloadTask *DownloadTask
}

type Service interface {
	Create(ctx context.Context, req CreateParams) (CreateResult, error)
	Get(ctx context.Context, req GetParams) (GetResult, error)
	List(ctx context.Context, req ListParams) (ListResult, error)
	Update(ctx context.Context, req UpdateParams) (UpdateResult, error)
	Delete(ctx context.Context, req DeleteParams) error
}

type TxManager interface {
	DoInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type CreatedEvent struct {
	DownloadTask DownloadTask
}

type CreatedProducer interface {
	Produce(ctx context.Context, event CreatedEvent) error
}

type Store interface {
	Create(ctx context.Context, task DownloadTask) (uint64, error)
	GetTaskByID(ctx context.Context, id uint64) (*DownloadTask, error)
	GetTaskByIDWithLock(ctx context.Context, id uint64) (*DownloadTask, error)
	GetTaskListOfUser(ctx context.Context, userID, offset, limit uint64) ([]DownloadTask, error)
	GetTaskCountOfUser(ctx context.Context, userID uint64) (uint64, error)
	Update(ctx context.Context, task DownloadTask) error
	DeleteTask(ctx context.Context, id uint64) error
}

type service struct {
	txManager TxManager
	store     Store
	producer  CreatedProducer
}

func NewService(txManager TxManager, store Store, producer CreatedProducer) Service {
	return &service{
		txManager: txManager,
		store:     store,
		producer:  producer,
	}
}

func (s *service) Create(ctx context.Context, req CreateParams) (CreateResult, error) {
	task := DownloadTask{
		OfAccountId:    req.UserID,
		DownloadType:   req.DownloadType,
		Url:            req.Url,
		DownloadStatus: DOWNLOAD_STATUS_PENDING,
		Metadata:       make(map[string]any),
	}

	if err := s.txManager.DoInTx(ctx, func(ctx context.Context) error {
		id, err := s.store.Create(ctx, task)
		if err != nil {
			return err
		}
		task.Id = id
		if err := s.producer.Produce(ctx, CreatedEvent{DownloadTask: task}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return CreateResult{}, err
	}

	return CreateResult{DownloadTask: &task}, nil
}

func (s *service) Get(ctx context.Context, req GetParams) (GetResult, error) {
	task, err := s.store.GetTaskByID(ctx, req.DownloadTaskID)
	if err != nil {
		if stderrors.Is(err, errors.ErrNotFound) {
			return GetResult{}, &errors.Error{
				Code:    errors.ErrCodeNotFound,
				Message: "task not found",
			}
		}
		return GetResult{}, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "failed to get task",
			Cause:   err,
		}
	}

	if task.OfAccountId != req.UserID {
		return GetResult{}, &errors.Error{
			Code:    errors.ErrCodePermissionDenied,
			Message: "trying to get other's task",
		}
	}
	return GetResult{DownloadTask: task}, nil
}

func (s *service) List(ctx context.Context, req ListParams) (ListResult, error) {
	tasks, err := s.store.GetTaskListOfUser(ctx, req.UserID, req.Offset, req.Limit)
	if err != nil {
		return ListResult{}, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "failed to get task list",
			Cause:   err,
		}
	}

	totalCount, err := s.store.GetTaskCountOfUser(ctx, req.UserID)
	if err != nil {
		return ListResult{}, err
	}

	return ListResult{DownloadTasks: lo.Map(tasks, func(task DownloadTask, _ int) *DownloadTask { return &task }),
		TotalCount: totalCount}, nil
}

func (s *service) Update(ctx context.Context, req UpdateParams) (UpdateResult, error) {
	var task *DownloadTask

	if err := s.txManager.DoInTx(ctx, func(ctx context.Context) error {
		var err error
		task, err = s.store.GetTaskByIDWithLock(ctx, req.DownloadTaskId)
		if err != nil {
			if stderrors.Is(err, errors.ErrNotFound) {
				return &errors.Error{
					Code:    errors.ErrCodeNotFound,
					Message: "task not found",
				}
			}
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to get task",
				Cause:   err,
			}
		}
		if task.OfAccountId != req.UserID {
			return &errors.Error{
				Code:    errors.ErrCodePermissionDenied,
				Message: "trying to update other's task",
			}
		}
		task.Url = req.Url
		if err := s.store.Update(ctx, *task); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to update task",
				Cause:   err,
			}
		}
		return nil
	}); err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{DownloadTask: task}, nil
}

func (s *service) Delete(ctx context.Context, req DeleteParams) error {
	return s.txManager.DoInTx(ctx, func(ctx context.Context) error {
		task, err := s.store.GetTaskByIDWithLock(ctx, req.DownloadTask.Id)
		if err != nil {
			if stderrors.Is(err, errors.ErrNotFound) {
				return &errors.Error{
					Code:    errors.ErrCodeNotFound,
					Message: "task not found",
				}
			}
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to get task",
				Cause:   err,
			}
		}
		if task.OfAccountId != req.UserID {
			return &errors.Error{
				Code:    errors.ErrCodePermissionDenied,
				Message: "trying to delete other's task",
			}
		}
		if err := s.store.DeleteTask(ctx, req.DownloadTask.Id); err != nil {
			return &errors.Error{
				Code:    errors.ErrCodeInternal,
				Message: "failed to delete task",
				Cause:   err,
			}
		}
		return nil
	})
}
