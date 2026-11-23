package sqlite

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
	task "github.com/yuisofull/goload/internal/task"
)

// errAttemptNeedsTx is returned when the lock or the save is attempted
// without the connection DoInTx stores in ctx.
var errAttemptNeedsTx = stderrors.New("sqlite: attempt state must be locked and saved inside TxManager.DoInTx")

// lockTaskRowSQL changes nothing. As the first statement of a transaction it
// takes the database write lock, waiting with the busy timeout, before the
// row is read.
const lockTaskRowSQL = `UPDATE tasks SET attempt_generation = attempt_generation WHERE id = ?`

const selectAttemptStateSQL = `SELECT status, owner_id, lease_expires_at, attempt_generation, retry_count, next_attempt_at,
       max_retries, file_name, storage_type, storage_path, checksum_type, checksum_value,
       progress, downloaded_bytes, total_bytes, error_message, completed_at,
       result_content_type, result_file_size, created_at
FROM tasks
WHERE id = ?`

const saveAttemptStateSQL = `UPDATE tasks
SET status = ?, owner_id = ?, lease_expires_at = ?, attempt_generation = ?, retry_count = ?, next_attempt_at = ?,
    file_name = ?, storage_type = ?, storage_path = ?, checksum_type = ?, checksum_value = ?,
    progress = ?, downloaded_bytes = ?, total_bytes = ?, error_message = ?, completed_at = ?,
    result_content_type = ?, result_file_size = ?
WHERE id = ?`

type attemptRepo struct {
	pool *sqlitex.Pool
}

// NewAttemptRepo returns the SQLite implementation of task.AttemptRepository.
func NewAttemptRepo(pool *sqlitex.Pool) task.AttemptRepository {
	return &attemptRepo{pool: pool}
}

func (r *attemptRepo) GetAttemptState(ctx context.Context, taskID uint64) (*task.AttemptState, error) {
	if conn, ok := ctx.Value(connKey{}).(*sqlite.Conn); ok {
		return readAttemptState(conn, taskID)
	}
	conn := r.pool.Get(ctx)
	if conn == nil {
		return nil, context.DeadlineExceeded
	}
	defer r.pool.Put(conn)
	return readAttemptState(conn, taskID)
}

func (r *attemptRepo) LockAttemptState(ctx context.Context, taskID uint64) (*task.AttemptState, error) {
	conn, ok := ctx.Value(connKey{}).(*sqlite.Conn)
	if !ok {
		return nil, errAttemptNeedsTx
	}
	// A transaction that reads first and writes later fails with SQLITE_BUSY
	// when another writer commits in between. Writing first takes the write
	// lock up front, so concurrent transitions of a task queue here.
	if err := sqlitex.Execute(conn, lockTaskRowSQL, &sqlitex.ExecOptions{Args: []any{taskID}}); err != nil {
		return nil, err
	}
	return readAttemptState(conn, taskID)
}

func (r *attemptRepo) SaveAttemptState(ctx context.Context, state *task.AttemptState) error {
	conn, ok := ctx.Value(connKey{}).(*sqlite.Conn)
	if !ok {
		return errAttemptNeedsTx
	}

	// sqlitex binds an untyped nil as NULL but prints a nil pointer as the
	// text "<nil>", so every nullable value is converted to any here.
	var checksumType, checksumValue, errorMessage, completedAt any
	if state.Checksum != nil {
		checksumType, checksumValue = state.Checksum.ChecksumType, state.Checksum.ChecksumValue
	}
	if state.ErrorMessage != nil {
		errorMessage = *state.ErrorMessage
	}
	if state.CompletedAt != nil {
		// Same format the task repository writes for completed_at.
		completedAt = state.CompletedAt.UTC().Format(time.RFC3339)
	}

	return sqlitex.Execute(conn, saveAttemptStateSQL, &sqlitex.ExecOptions{
		Args: []any{
			string(state.Status), nullableText(state.OwnerID), nullableTime(state.LeaseExpiresAt),
			state.Generation, state.RetryCount, nullableTime(state.NextAttemptAt),
			state.FileName, string(state.StorageType), state.StoragePath, checksumType, checksumValue,
			state.Progress.Progress, state.Progress.DownloadedBytes, state.Progress.TotalBytes,
			errorMessage, completedAt,
			state.ContentType, state.FileSize,
			state.TaskID,
		},
	})
}

// nullableText binds an empty string as NULL.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableTime binds a nil time as NULL and any other time as fixed-width
// UTC text, so comparing the stored strings compares the times.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(outboxTimeLayout)
}

func readAttemptState(conn *sqlite.Conn, taskID uint64) (*task.AttemptState, error) {
	var state *task.AttemptState
	err := sqlitex.Execute(conn, selectAttemptStateSQL, &sqlitex.ExecOptions{
		Args: []any{taskID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			var err error
			state, err = scanAttemptState(stmt, taskID)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, errors.ErrNotFound
	}
	return state, nil
}

// scanAttemptState maps one selectAttemptStateSQL row. Column order: status,
// owner_id, lease_expires_at, attempt_generation, retry_count,
// next_attempt_at, max_retries, file_name, storage_type, storage_path,
// checksum_type, checksum_value, progress, downloaded_bytes, total_bytes,
// error_message, completed_at, result_content_type, result_file_size.
func scanAttemptState(stmt *sqlite.Stmt, taskID uint64) (*task.AttemptState, error) {
	state := &task.AttemptState{
		TaskID:     taskID,
		Status:     task.TaskStatus(stmt.ColumnText(0)),
		OwnerID:    stmt.ColumnText(1), // NULL reads as ""
		Generation: uint64(stmt.ColumnInt64(3)),
		RetryCount: stmt.ColumnInt(4),
		MaxRetries: stmt.ColumnInt(6),
		FileName:   stmt.ColumnText(7),
		// Kept exactly as stored (no normalization), so a save writes the
		// same value back.
		StorageType: storage.Type(stmt.ColumnText(8)),
		StoragePath: stmt.ColumnText(9),
		// Result identity; a NULL content type reads as "".
		ContentType: stmt.ColumnText(17),
		FileSize:    stmt.ColumnInt64(18),
		Progress: task.DownloadProgress{
			Progress:        stmt.ColumnFloat(12),
			DownloadedBytes: stmt.ColumnInt64(13),
			TotalBytes:      stmt.ColumnInt64(14),
		},
	}
	state.CreatedAt = parseSqliteTime(stmt.ColumnText(19))

	var err error
	if state.LeaseExpiresAt, err = columnTime(stmt, 2); err != nil {
		return nil, fmt.Errorf("parse lease_expires_at: %w", err)
	}
	if state.NextAttemptAt, err = columnTime(stmt, 5); err != nil {
		return nil, fmt.Errorf("parse next_attempt_at: %w", err)
	}
	if stmt.ColumnType(10) != sqlite.SQLITE_NULL || stmt.ColumnType(11) != sqlite.SQLITE_NULL {
		state.Checksum = &task.ChecksumInfo{
			ChecksumType:  stmt.ColumnText(10),
			ChecksumValue: stmt.ColumnText(11),
		}
	}
	if stmt.ColumnType(15) != sqlite.SQLITE_NULL {
		message := stmt.ColumnText(15)
		state.ErrorMessage = &message
	}
	if stmt.ColumnType(16) != sqlite.SQLITE_NULL {
		completedAt := parseSqliteTime(stmt.ColumnText(16))
		state.CompletedAt = &completedAt
	}
	return state, nil
}

// columnTime reads a nullable fixed-width UTC timestamp column.
func columnTime(stmt *sqlite.Stmt, col int) (*time.Time, error) {
	if stmt.ColumnType(col) == sqlite.SQLITE_NULL {
		return nil, nil
	}
	parsed, err := time.Parse(outboxTimeLayout, stmt.ColumnText(col))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
