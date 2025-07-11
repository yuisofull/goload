package downloadtaskmysql

import (
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/internal/task/mysql/sqlc"
)

type store struct {
	queries *sqlc.Queries
}

func NewStore(db *sql.DB) *store {
	return &store{
		queries: sqlc.New(db),
	}
}

func (s *store) Create(ctx context.Context, task task.DownloadTask) (uint64, error) {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}

	metadata, err := json.Marshal(task.Metadata)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	result, err := q.CreateDownloadTask(ctx, sqlc.CreateDownloadTaskParams{
		OfAccountID:    task.OfAccountId,
		DownloadType:   int16(task.DownloadType),
		Url:            task.Url,
		DownloadStatus: int16(task.DownloadStatus),
		Metadata:       metadata,
	})
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()

	return uint64(id), err
}

func (s *store) GetTaskByID(ctx context.Context, id uint64) (*task.DownloadTask, error) {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	row, err := q.GetDownloadTaskByID(ctx, id)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, errors.ErrNotFound
		}
		return nil, err
	}
	metadata := make(map[string]any)
	if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}
	return &task.DownloadTask{
		Id:             row.ID,
		OfAccountId:    row.OfAccountID,
		DownloadType:   task.DownloadType(row.DownloadType),
		Url:            row.Url,
		DownloadStatus: task.DownloadStatus(row.DownloadStatus),
		Metadata:       metadata,
	}, nil
}

func (s *store) GetTaskByIDWithLock(ctx context.Context, id uint64) (*task.DownloadTask, error) {
	q := s.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	row, err := q.GetDownloadTaskByIDWithLock(ctx, id)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, errors.ErrNotFound
		}
		return nil, err
	}
	metadata := make(map[string]any)
	if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}
	return &task.DownloadTask{
		Id:             row.ID,
		OfAccountId:    row.OfAccountID,
		DownloadType:   task.DownloadType(row.DownloadType),
		Url:            row.Url,
		DownloadStatus: task.DownloadStatus(row.DownloadStatus),
		Metadata:       metadata,
	}, nil
}

func (s *store) GetTaskListOfUser(ctx context.Context, userID, offset, limit uint64) ([]task.DownloadTask, error) {
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
		return nil, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "failed to get download task list of user",
			Cause:   err,
		}
	}
	tasks := make([]task.DownloadTask, len(rows))
	for i, row := range rows {
		metadata := make(map[string]any)
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
		tasks[i] = task.DownloadTask{
			Id:             row.ID,
			OfAccountId:    row.OfAccountID,
			DownloadType:   task.DownloadType(row.DownloadType),
			Url:            row.Url,
			DownloadStatus: task.DownloadStatus(row.DownloadStatus),
			Metadata:       metadata,
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

	return uint64(count), err
}

func (s *store) Update(ctx context.Context, task task.DownloadTask) error {
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
