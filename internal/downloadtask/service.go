package downloadtask

import (
	"context"
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
		Metadata:       "{}",
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
	//TODO implement me
	panic("implement me")
}

func (s *service) Update(ctx context.Context, req UpdateParams) (UpdateResult, error) {
	//TODO implement me
	panic("implement me")
}

func (s *service) Delete(ctx context.Context, req DeleteParams) error {
	//TODO implement me
	panic("implement me")
}
