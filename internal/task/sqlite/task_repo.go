package sqlite

import (
	"context"
	"encoding/json"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
	task "github.com/yuisofull/goload/internal/task"
)

type taskRepo struct {
	pool *sqlitex.Pool
}

func NewTaskRepo(pool *sqlitex.Pool) task.Repository {
	return &taskRepo{pool: pool}
}

func (r *taskRepo) getConn(ctx context.Context) *sqlite.Conn {
	if conn, ok := ctx.Value(connKey{}).(*sqlite.Conn); ok {
		return conn
	}
	return nil
}

func (r *taskRepo) withConn(ctx context.Context, fn func(conn *sqlite.Conn) error) error {
	conn := r.getConn(ctx)
	if conn != nil {
		return fn(conn)
	}
	conn = r.pool.Get(ctx)
	if conn == nil {
		return context.DeadlineExceeded
	}
	defer r.pool.Put(conn)
	return fn(conn)
}

func (r *taskRepo) Create(ctx context.Context, t *task.Task) (*task.Task, error) {
	sourceAuth, _ := json.Marshal(t.SourceAuth)
	metadata, _ := json.Marshal(t.Metadata)
	var headers []byte
	if t.SourceAuth != nil {
		headers, _ = json.Marshal(t.SourceAuth.Headers)
	}

	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		err := sqlitex.Execute(
			conn,
			`INSERT INTO tasks (of_account_id, file_name, source_url, source_type, source_auth, headers,
                   storage_type, storage_path, status,
                   checksum_type, checksum_value,
                   concurrency, max_speed, max_retries, timeout, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
			&sqlitex.ExecOptions{
				Args: []any{
					t.OfAccountID, t.FileName, t.SourceURL, string(t.SourceType), sourceAuth, headers,
					string(t.StorageType), t.StoragePath, string(t.Status),
					getChecksumType(t), getChecksumValue(t),
					getConcurrency(t), getMaxSpeed(t), getMaxRetries(t), getTimeout(t), metadata,
				},
			},
		)
		if err != nil {
			return err
		}
		t.ID = uint64(conn.LastInsertRowID())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *taskRepo) Update(ctx context.Context, t *task.Task) (*task.Task, error) {
	return r.updateInternal(ctx, t)
}

func (r *taskRepo) updateInternal(ctx context.Context, t *task.Task) (*task.Task, error) {
	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		if t.Progress != nil {
			if t.Progress.Progress > 0 {
				if err := sqlitex.Execute(
					conn,
					`UPDATE tasks SET progress = ? WHERE id = ?`,
					&sqlitex.ExecOptions{Args: []any{t.Progress.Progress, t.ID}},
				); err != nil {
					return err
				}
			}
			if t.Progress.TotalBytes > 0 {
				if err := sqlitex.Execute(
					conn,
					`UPDATE tasks SET total_bytes = ? WHERE id = ?`,
					&sqlitex.ExecOptions{Args: []any{t.Progress.TotalBytes, t.ID}},
				); err != nil {
					return err
				}
			}
			if t.Progress.DownloadedBytes > 0 {
				if err := sqlitex.Execute(
					conn,
					`UPDATE tasks SET downloaded_bytes = ? WHERE id = ?`,
					&sqlitex.ExecOptions{Args: []any{t.Progress.DownloadedBytes, t.ID}},
				); err != nil {
					return err
				}
			}
		}
		if t.FileName != "" {
			if err := sqlitex.Execute(
				conn,
				`UPDATE tasks SET file_name = ? WHERE id = ?`,
				&sqlitex.ExecOptions{Args: []any{t.FileName, t.ID}},
			); err != nil {
				return err
			}
		}
		if t.Status != "" {
			if err := sqlitex.Execute(
				conn,
				`UPDATE tasks SET status = ? WHERE id = ?`,
				&sqlitex.ExecOptions{Args: []any{string(t.Status), t.ID}},
			); err != nil {
				return err
			}
		}
		if t.CompletedAt != nil {
			if err := sqlitex.Execute(
				conn,
				`UPDATE tasks SET completed_at = ? WHERE id = ?`,
				&sqlitex.ExecOptions{Args: []any{t.CompletedAt.Format(time.RFC3339), t.ID}},
			); err != nil {
				return err
			}
		}
		if t.ErrorMessage != nil && *t.ErrorMessage != "" {
			if err := sqlitex.Execute(
				conn,
				`UPDATE tasks SET error_message = ? WHERE id = ?`,
				&sqlitex.ExecOptions{Args: []any{*t.ErrorMessage, t.ID}},
			); err != nil {
				return err
			}
		}
		if t.StorageType != "" || t.StoragePath != "" {
			if err := sqlitex.Execute(
				conn,
				`UPDATE tasks SET storage_type = ?, storage_path = ? WHERE id = ?`,
				&sqlitex.ExecOptions{Args: []any{string(t.StorageType), t.StoragePath, t.ID}},
			); err != nil {
				return err
			}
		}
		if t.Metadata != nil {
			metadata, _ := json.Marshal(t.Metadata)
			if err := sqlitex.Execute(
				conn,
				`UPDATE tasks SET metadata = ? WHERE id = ?`,
				&sqlitex.ExecOptions{Args: []any{metadata, t.ID}},
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *taskRepo) GetByID(ctx context.Context, id uint64) (*task.Task, error) {
	var t *task.Task
	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `SELECT * FROM tasks WHERE id = ?`, &sqlitex.ExecOptions{
			Args: []any{id},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				var err error
				t, err = scanTask(stmt)
				return err
			},
		})
	})
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.ErrNotFound
	}
	return t, nil
}

func (r *taskRepo) ListByAccountID(
	ctx context.Context,
	filter task.TaskFilter,
	limit, offset uint32,
) ([]*task.Task, error) {
	var tasks []*task.Task
	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(
			conn,
			`SELECT * FROM tasks WHERE of_account_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
			&sqlitex.ExecOptions{
				Args: []any{filter.OfAccountID, int64(limit), int64(offset)},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					t, err := scanTask(stmt)
					if err != nil {
						return err
					}
					tasks = append(tasks, t)
					return nil
				},
			},
		)
	})
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepo) GetTaskCountOfAccount(ctx context.Context, ofAccountID uint64) (uint64, error) {
	var count int64
	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `SELECT COUNT(*) FROM tasks WHERE of_account_id = ?`, &sqlitex.ExecOptions{
			Args: []any{ofAccountID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				count = stmt.ColumnInt64(0)
				return nil
			},
		})
	})
	return uint64(count), err
}

func (r *taskRepo) Delete(ctx context.Context, id uint64) error {
	return r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `DELETE FROM tasks WHERE id = ?`, &sqlitex.ExecOptions{Args: []any{id}})
	})
}

func scanTask(stmt *sqlite.Stmt) (*task.Task, error) {
	t := &task.Task{}
	cols := make(map[string]int)
	for i := range stmt.ColumnCount() {
		cols[stmt.ColumnName(i)] = i
	}

	t.ID = uint64(stmt.ColumnInt64(cols["id"]))
	t.OfAccountID = uint64(stmt.ColumnInt64(cols["of_account_id"]))
	t.FileName = stmt.ColumnText(cols["file_name"])
	t.SourceURL = stmt.ColumnText(cols["source_url"])
	t.SourceType = task.SourceType(stmt.ColumnText(cols["source_type"]))

	sourceAuthBytes := make([]byte, stmt.ColumnLen(cols["source_auth"]))
	stmt.ColumnBytes(cols["source_auth"], sourceAuthBytes)
	json.Unmarshal(sourceAuthBytes, &t.SourceAuth)

	metadataBytes := make([]byte, stmt.ColumnLen(cols["metadata"]))
	stmt.ColumnBytes(cols["metadata"], metadataBytes)
	json.Unmarshal(metadataBytes, &t.Metadata)

	t.StorageType = storage.TypeValue(stmt.ColumnText(cols["storage_type"]))
	t.StoragePath = stmt.ColumnText(cols["storage_path"])
	t.Status = task.TaskStatus(stmt.ColumnText(cols["status"]))

	t.Progress = &task.DownloadProgress{
		Progress:        stmt.ColumnFloat(cols["progress"]),
		DownloadedBytes: stmt.ColumnInt64(cols["downloaded_bytes"]),
		TotalBytes:      stmt.ColumnInt64(cols["total_bytes"]),
	}

	t.Checksum = &task.ChecksumInfo{
		ChecksumType:  stmt.ColumnText(cols["checksum_type"]),
		ChecksumValue: stmt.ColumnText(cols["checksum_value"]),
	}

	if stmt.ColumnType(cols["error_message"]) != sqlite.SQLITE_NULL {
		errMsg := stmt.ColumnText(cols["error_message"])
		t.ErrorMessage = &errMsg
	}

	t.CreatedAt = parseSqliteTime(stmt.ColumnText(cols["created_at"]))
	t.UpdatedAt = parseSqliteTime(stmt.ColumnText(cols["updated_at"]))
	if stmt.ColumnType(cols["completed_at"]) != sqlite.SQLITE_NULL {
		ct := parseSqliteTime(stmt.ColumnText(cols["completed_at"]))
		t.CompletedAt = &ct
	}

	return t, nil
}

func parseSqliteTime(s string) time.Time {
	formats := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z",
	}
	for _, f := range formats {
		t, err := time.ParseInLocation(f, s, time.UTC)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

func getChecksumType(t *task.Task) any {
	if t.Checksum != nil {
		return t.Checksum.ChecksumType
	}
	return nil
}

func getChecksumValue(t *task.Task) any {
	if t.Checksum != nil {
		return t.Checksum.ChecksumValue
	}
	return nil
}

func getConcurrency(t *task.Task) any {
	if t.DownloadOptions != nil {
		return t.DownloadOptions.Concurrency
	}
	return nil
}

func getMaxSpeed(t *task.Task) any {
	if t.DownloadOptions != nil && t.DownloadOptions.MaxSpeed != nil {
		return *t.DownloadOptions.MaxSpeed
	}
	return nil
}

func getMaxRetries(t *task.Task) any {
	if t.DownloadOptions != nil {
		return t.DownloadOptions.MaxRetries
	}
	return nil
}

func getTimeout(t *task.Task) any {
	if t.DownloadOptions != nil && t.DownloadOptions.Timeout != nil {
		return *t.DownloadOptions.Timeout
	}
	return nil
}
