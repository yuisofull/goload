package downloadtaskmysql

import (
	"context"
	"database/sql"
	"github.com/yuisofull/goload/internal/downloadtask"
	"github.com/yuisofull/goload/internal/downloadtask/mysql/sqlc"
)

type store struct {
	queries *sqlc.Queries
}

func NewStore(db *sql.DB) *store {
	return &store{
		queries: sqlc.New(db),
	}
}

func (s *store) Create(ctx context.Context, task downloadtask.DownloadTask) (uint64, error) {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	result, err := q.CreateDownloadTask(ctx, sqlc.CreateDownloadTaskParams{
		OfAccountID:    task.OfAccountId,
		DownloadType:   int16(task.DownloadType),
		Url:            task.Url,
		DownloadStatus: int16(task.DownloadStatus),
		Metadata:       task.Metadata,
	})
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

func (s *store) GetTaskListOfUser(ctx context.Context, userID, offset, limit uint64) ([]downloadtask.DownloadTask, error) {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	rows, err := q.GetDownloadTaskListOfUser(ctx, sqlc.GetDownloadTaskListOfUserParams{
		OfAccountID: userID,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		return nil, err
	}
	tasks := make([]downloadtask.DownloadTask, len(rows))
	for i, row := range rows {
		tasks[i] = downloadtask.DownloadTask{
			Id:             row.ID,
			OfAccountId:    row.OfAccountID,
			DownloadType:   downloadtask.DownloadType(row.DownloadType),
			Url:            row.Url,
			DownloadStatus: downloadtask.DownloadStatus(row.DownloadStatus),
		}
	}
	return tasks, nil
}

func (s *store) GetTaskCountOfUser(ctx context.Context, userID uint64) (uint64, error) {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	count, err := q.GetDownloadTaskCountOfUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	return uint64(count), nil
}

func (s *store) Update(ctx context.Context, task downloadtask.DownloadTask) error {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	return q.UpdateDownloadTask(ctx, sqlc.UpdateDownloadTaskParams{
		Url: task.Url,
		ID:  task.Id,
	})
}

func (s *store) DeleteTask(ctx context.Context, id uint64) error {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	return q.DeleteDownloadTask(ctx, id)
}
