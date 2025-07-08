package downloadtask

import (
	"context"
	"github.com/samber/lo"
	"github.com/yuisofull/goload/internal/errors"
)

type CreateParams struct {
	Token        string
	DownloadType DownloadType
	Url          string
}

type CreateResult struct {
	DownloadTask *DownloadTask
}

type ListParams struct {
	Token  string
	Offset uint64
	Limit  uint64
}

type ListResult struct {
	DownloadTasks []*DownloadTask
	TotalCount    uint64
}

type UpdateParams struct {
	Token          string
	DownloadTaskId uint64
	Url            string
}

type UpdateResult struct {
	DownloadTask *DownloadTask
}

type DeleteParams struct {
	Token        string
	DownloadTask *DownloadTask
}

type Service interface {
	Create(ctx context.Context, req CreateParams) (CreateResult, error)
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

type TokenManager interface {
	GetAccountIDFrom(token string) (uint64, error)
}

type service struct {
	txManager    TxManager
	store        Store
	producer     CreatedProducer
	tokenManager TokenManager
}

func (s *service) Create(ctx context.Context, req CreateParams) (CreateResult, error) {
	accountID, err := s.tokenManager.GetAccountIDFrom(req.Token)
	if err != nil {
		return CreateResult{}, err
	}

	task := DownloadTask{
		OfAccountId:    accountID,
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

func (s *service) List(ctx context.Context, req ListParams) (ListResult, error) {
	accountID, err := s.tokenManager.GetAccountIDFrom(req.Token)
	if err != nil {
		return ListResult{}, err
	}

	tasks, err := s.store.GetTaskListOfUser(ctx, accountID, req.Offset, req.Limit)
	if err != nil {
		return ListResult{}, err
	}

	totalCount, err := s.store.GetTaskCountOfUser(ctx, accountID)
	if err != nil {
		return ListResult{}, err
	}

	return ListResult{DownloadTasks: lo.Map(tasks, func(task DownloadTask, _ int) *DownloadTask { return &task }),
		TotalCount: totalCount}, nil
}

func (s *service) Update(ctx context.Context, req UpdateParams) (UpdateResult, error) {
	accountID, err := s.tokenManager.GetAccountIDFrom(req.Token)
	if err != nil {
		return UpdateResult{}, err
	}
	var task *DownloadTask

	if err := s.txManager.DoInTx(ctx, func(ctx context.Context) error {
		task, err = s.store.GetTaskByIDWithLock(ctx, req.DownloadTaskId)
		if err != nil {
			return err
		}
		if task.OfAccountId != accountID {
			return errors.NewServiceError(errors.ErrCodePermissionDenied, "trying to update other's task", nil)
		}
		task.Url = req.Url
		if err := s.store.Update(ctx, *task); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{DownloadTask: task}, nil
}

func (s *service) Delete(ctx context.Context, req DeleteParams) error {
	accountID, err := s.tokenManager.GetAccountIDFrom(req.Token)
	if err != nil {
		return err
	}

	if err := s.txManager.DoInTx(ctx, func(ctx context.Context) error {
		task, err := s.store.GetTaskByIDWithLock(ctx, req.DownloadTask.Id)
		if err != nil {
			return err
		}
		if task.OfAccountId != accountID {
			return errors.NewServiceError(errors.ErrCodePermissionDenied, "trying to delete other's task", nil)
		}
		if err := s.store.DeleteTask(ctx, req.DownloadTask.Id); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}
