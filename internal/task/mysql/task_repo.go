package mysql

import (
	"context"
	"database/sql"
	stderrs "errors"
	"fmt"
	"time"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
	task "github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/internal/task/mysql/sqlc"
)

type taskRepo struct {
	queries *sqlc.Queries
	db      *sql.DB
}

func NewTaskRepo(db *sql.DB) task.Repository {
	return &taskRepo{
		queries: sqlc.New(db),
		db:      db,
	}
}

func (r *taskRepo) Create(ctx context.Context, t *task.Task) (*task.Task, error) {
	q := r.queries
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		q = q.WithTx(tx)
	}
	sourceAuth, err := toJSON(t.SourceAuth)
	if err != nil {
		return nil, fmt.Errorf("marshal SourceAuth: %w", err)
	}
	metadata, err := toJSON(t.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal Metadata: %w", err)
	}
	headers, err := toJSON(t.SourceAuth.Headers)
	if err != nil {
		return nil, fmt.Errorf("marshal Headers: %w", err)
	}

	result, err := q.CreateTask(ctx, sqlc.CreateTaskParams{
		OfAccountID:   t.OfAccountID,
		FileName:      t.FileName,
		SourceUrl:     t.SourceURL,
		SourceType:    string(t.SourceType),
		SourceAuth:    sourceAuth,
		Headers:       headers,
		StorageType:   string(t.StorageType),
		StoragePath:   t.StoragePath,
		Status:        string(t.Status),
		ChecksumType:  sql.NullString{String: t.Checksum.ChecksumType, Valid: t.Checksum != nil},
		ChecksumValue: sql.NullString{String: t.Checksum.ChecksumValue, Valid: t.Checksum != nil},
		Concurrency:   sql.NullInt32{Int32: int32(t.DownloadOptions.Concurrency), Valid: t.DownloadOptions != nil},
		MaxSpeed:      sql.NullInt64{Int64: *t.DownloadOptions.MaxSpeed, Valid: t.DownloadOptions != nil && t.DownloadOptions.MaxSpeed != nil},
		MaxRetries:    int32(t.DownloadOptions.MaxRetries),
		Timeout:       sql.NullInt32{Int32: int32(*t.DownloadOptions.Timeout), Valid: t.DownloadOptions != nil && t.DownloadOptions.Timeout != nil},
		Metadata:      metadata,
	})
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	t.ID = uint64(id)
	return t, nil
}

func (r *taskRepo) Update(ctx context.Context, t *task.Task) (*task.Task, error) {
	q := r.queries
	var (
		opentx bool
		err    error
		tx     *sql.Tx
	)

	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		opentx = true
	} else {
		tx, err = r.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
	}
	q = q.WithTx(tx)

	if t.Progress != nil {
		if t.Progress.Progress > 0 {
			if err = q.UpdateTaskProgress(ctx, sqlc.UpdateTaskProgressParams{
				ID:       t.ID,
				Progress: sql.NullFloat64{Float64: t.Progress.Progress, Valid: true},
			}); err != nil {
				return nil, err
			}
		}
		if t.Progress.TotalBytes > 0 {
			if err = q.UpdateTaskTotalBytes(ctx, sqlc.UpdateTaskTotalBytesParams{
				ID:         t.ID,
				TotalBytes: sql.NullInt64{Int64: t.Progress.TotalBytes, Valid: true},
			}); err != nil {
				return nil, err
			}
		}
		if t.Progress.DownloadedBytes > 0 {
			if err = q.UpdateTaskDownloadedBytes(ctx, sqlc.UpdateTaskDownloadedBytesParams{
				ID:              t.ID,
				DownloadedBytes: sql.NullInt64{Int64: t.Progress.DownloadedBytes, Valid: true},
			}); err != nil {
				return nil, err
			}
		}

	}
	if t.FileName != "" {
		if err = q.UpdateFileName(ctx, sqlc.UpdateFileNameParams{
			ID:       t.ID,
			FileName: t.FileName,
		}); err != nil {
			return nil, err
		}
	}

	if t.Status != "" {
		if err = q.UpdateTaskStatus(ctx, sqlc.UpdateTaskStatusParams{
			ID:     t.ID,
			Status: string(t.Status),
		}); err != nil {
			return nil, err
		}
	}

	if t.CompletedAt != nil {
		if err = q.UpdateTaskCompletedAt(ctx, sqlc.UpdateTaskCompletedAtParams{
			ID:          t.ID,
			CompletedAt: sql.NullTime{Time: *t.CompletedAt, Valid: true},
		}); err != nil {
			return nil, err
		}
	}

	if t.ErrorMessage != nil && *t.ErrorMessage != "" {
		if err = q.UpdateTaskError(ctx, sqlc.UpdateTaskErrorParams{
			ID:           t.ID,
			ErrorMessage: sql.NullString{String: *t.ErrorMessage, Valid: true},
		}); err != nil {
			return nil, err
		}
	}

	if t.StorageType != "" || t.StoragePath != "" {
		if err = q.UpdateStorageInfo(ctx, sqlc.UpdateStorageInfoParams{
			ID:          t.ID,
			StorageType: string(t.StorageType),
			StoragePath: t.StoragePath,
		}); err != nil {
			return nil, err
		}
	}

	if opentx {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}

	return t, nil
}

func (r *taskRepo) GetByID(ctx context.Context, id uint64) (*task.Task, error) {
	q := r.queries
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		q = q.WithTx(tx)
	}
	t, err := q.GetTaskById(ctx, id)
	if err != nil {
		if stderrs.Is(err, sql.ErrNoRows) {
			return nil, errors.ErrNotFound
		}
		return nil, err
	}
	return toTask(t)
}

func (r *taskRepo) ListByAccountID(ctx context.Context, filter task.TaskFilter, limit, offset uint32) ([]*task.Task, error) {
	q := r.queries
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		q = q.WithTx(tx)
	}
	tasks, err := q.ListTasks(ctx, sqlc.ListTasksParams{
		OfAccountID: filter.OfAccountID,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		return nil, err
	}
	return toTasks(tasks)
}

func (r *taskRepo) GetTaskCountOfAccount(ctx context.Context, ofAccountID uint64) (uint64, error) {
	q := r.queries
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		q = q.WithTx(tx)
	}
	cnt, err := q.GetTaskCountByAccountId(ctx, ofAccountID)
	return uint64(cnt), err
}

func (r *taskRepo) Delete(ctx context.Context, id uint64) error {
	q := r.queries
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		q = q.WithTx(tx)
	}
	return q.DeleteTask(ctx, id)
}

func toTask(t sqlc.Task) (*task.Task, error) {
	sourceAuth, err := fromJSON[task.AuthConfig](t.SourceAuth)
	if err != nil {
		return nil, fmt.Errorf("unmarshal SourceAuth: %w", err)
	}
	metadata, err := fromJSON[map[string]interface{}](t.Metadata)
	if err != nil {
		return nil, fmt.Errorf("unmarshal Metadata: %w", err)
	}

	progress := &task.DownloadProgress{
		Progress:        t.Progress.Float64,
		DownloadedBytes: t.DownloadedBytes.Int64,
		TotalBytes:      t.TotalBytes.Int64,
	}

	checksum := &task.ChecksumInfo{
		ChecksumType:  t.ChecksumType.String,
		ChecksumValue: t.ChecksumValue.String,
	}

	return &task.Task{
		ID:          t.ID,
		OfAccountID: t.OfAccountID,
		FileName:    t.FileName,
		SourceURL:   t.SourceUrl,
		SourceType:  task.SourceType(t.SourceType),
		SourceAuth:  sourceAuth,
		StorageType: storage.TypeValue(t.StorageType),
		StoragePath: t.StoragePath,
		Checksum:    checksum,
		Status:      task.TaskStatus(t.Status),
		Progress:    progress,
		ErrorMessage: func() *string {
			if t.ErrorMessage.Valid {
				return &t.ErrorMessage.String
			}
			return nil
		}(),
		Metadata: *metadata,
		CreatedAt: func() time.Time {
			if t.CreatedAt.Valid {
				return t.CreatedAt.Time
			}
			return time.Time{}
		}(),
		UpdatedAt: func() time.Time {
			if t.UpdatedAt.Valid {
				return t.UpdatedAt.Time
			}
			return time.Time{}
		}(),
		CompletedAt: func() *time.Time {
			if t.CompletedAt.Valid {
				return &t.CompletedAt.Time
			}
			return nil
		}(),
	}, nil
}

func toTasks(tasks []sqlc.Task) ([]*task.Task, error) {
	var res []*task.Task
	for _, t := range tasks {
		taskItem, err := toTask(t)
		if err != nil {
			return nil, err
		}
		res = append(res, taskItem)
	}
	return res, nil
}
