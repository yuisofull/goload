package mysql

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/yuisofull/goload/internal/errors"
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
	fileInfo, err := toJSON(t.FileInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal FileInfo: %w", err)
	}
	progress, err := toJSON(t.Progress)
	if err != nil {
		return nil, fmt.Errorf("marshal Progress: %w", err)
	}
	options, err := toJSON(t.Options)
	if err != nil {
		return nil, fmt.Errorf("marshal Options: %w", err)
	}
	tags, err := toJSON(t.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal Tags: %w", err)
	}
	metadata, err := toJSON(t.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal Metadata: %w", err)
	}

	result, err := q.CreateTask(ctx, sqlc.CreateTaskParams{
		OfAccountID: t.OfAccountID,
		Name:        t.Name,
		Description: sql.NullString{String: t.Description, Valid: true},
		SourceUrl:   t.SourceURL,
		SourceType:  string(t.SourceType),
		SourceAuth:  sourceAuth,
		StorageType: string(t.StorageType),
		StoragePath: t.StoragePath,
		Status:      string(t.Status),
		FileInfo:    fileInfo,
		Progress:    progress,
		Options:     options,
		MaxRetries:  t.MaxRetries,
		Tags:        tags,
		Metadata:    metadata,
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
		openTx bool
		err    error
		tx     *sql.Tx
	)

	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		openTx = true
	} else {
		tx, err = r.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
	}
	q = q.WithTx(tx)

	fileInfo, err := toJSON(t.FileInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal FileInfo: %w", err)
	}
	progress, err := toJSON(t.Progress)
	if err != nil {
		return nil, fmt.Errorf("marshal Progress: %w", err)
	}

	if fileInfo != nil {
		if err = q.UpdateTaskFileInfo(ctx, sqlc.UpdateTaskFileInfoParams{
			ID:       t.ID,
			FileInfo: fileInfo,
		}); err != nil {
			return nil, err
		}
	}

	if progress != nil {
		if err = q.UpdateTaskProgress(ctx, sqlc.UpdateTaskProgressParams{
			ID:       t.ID,
			Progress: progress,
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

	if t.Error != "" {
		if err = q.UpdateTaskError(ctx, sqlc.UpdateTaskErrorParams{
			ID:    t.ID,
			Error: sql.NullString{String: t.Error, Valid: true},
		}); err != nil {
			return nil, err
		}
	}

	if t.RetryCount != 0 {
		if err = q.UpdateTaskRetryCount(ctx, sqlc.UpdateTaskRetryCountParams{
			ID:         t.ID,
			RetryCount: t.RetryCount,
		}); err != nil {
			return nil, err
		}
	}

	if openTx {
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
		if stderrors.Is(err, sql.ErrNoRows) {
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
	fileInfo, err := fromJSON[task.FileInfo](t.FileInfo)
	if err != nil {
		return nil, fmt.Errorf("unmarshal FileInfo: %w", err)
	}
	progress, err := fromJSON[task.DownloadProgress](t.Progress)
	if err != nil {
		return nil, fmt.Errorf("unmarshal Progress: %w", err)
	}
	options, err := fromJSON[task.DownloadOptions](t.Options)
	if err != nil {
		return nil, fmt.Errorf("unmarshal Options: %w", err)
	}
	tags, err := fromJSON[[]string](t.Tags)
	if err != nil {
		return nil, fmt.Errorf("unmarshal Tags: %w", err)
	}
	metadata, err := fromJSON[map[string]any](t.Metadata)
	if err != nil {
		return nil, fmt.Errorf("unmarshal Metadata: %w", err)
	}

	var completedAt *time.Time
	if t.CompletedAt.Valid {
		completedAt = &t.CompletedAt.Time
	}

	var description string
	if t.Description.Valid {
		description = t.Description.String
	}

	var errorMsg string
	if t.Error.Valid {
		errorMsg = t.Error.String
	}

	var createdAt time.Time
	if t.CreatedAt.Valid {
		createdAt = t.CreatedAt.Time
	}

	var updatedAt time.Time
	if t.UpdatedAt.Valid {
		updatedAt = t.UpdatedAt.Time
	}

	return &task.Task{
		ID:          t.ID,
		OfAccountID: t.OfAccountID,
		Name:        t.Name,
		Description: description,
		SourceURL:   t.SourceUrl,
		SourceType:  task.SourceType(t.SourceType),
		SourceAuth:  sourceAuth,
		StorageType: task.StorageType(t.StorageType),
		StoragePath: t.StoragePath,
		Status:      task.TaskStatus(t.Status),
		FileInfo:    fileInfo,
		Progress:    progress,
		Options:     options,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		CompletedAt: completedAt,
		Error:       errorMsg,
		RetryCount:  t.RetryCount,
		MaxRetries:  t.MaxRetries,
		Tags:        getOrEmpty(tags),
		Metadata:    getOrEmpty(metadata),
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
