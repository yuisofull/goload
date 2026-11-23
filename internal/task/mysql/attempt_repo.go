package mysql

import (
	"context"
	"database/sql"
	stderrors "errors"
	"time"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
	task "github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/internal/task/mysql/sqlc"
)

// errAttemptNeedsTx is returned when the lock or the save is attempted
// without the transaction DoInTx stores in ctx.
var errAttemptNeedsTx = stderrors.New("mysql: attempt state must be locked and saved inside TxManager.DoInTx")

type attemptRepo struct {
	db      *sql.DB
	queries *sqlc.Queries
}

// NewAttemptRepo returns the MySQL implementation of task.AttemptRepository.
// It uses the sqlc-generated queries and needs parseTime=true in the DSN,
// which cmd/task already sets.
func NewAttemptRepo(db *sql.DB) task.AttemptRepository {
	return &attemptRepo{db: db, queries: sqlc.New(db)}
}

// q binds the sqlc queries to the transaction DoInTx stored in ctx, or to the
// plain *sql.DB when there is none.
func (r *attemptRepo) q(ctx context.Context) *sqlc.Queries {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return r.queries.WithTx(tx)
	}
	return r.queries
}

func hasTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(*sql.Tx)
	return ok
}

func (r *attemptRepo) GetAttemptState(ctx context.Context, taskID uint64) (*task.AttemptState, error) {
	row, err := r.q(ctx).GetAttemptState(ctx, taskID)
	if err != nil {
		return nil, mapAttemptErr(err)
	}
	return attemptStateFromRow(taskID, row), nil
}

// LockAttemptState keeps the row locked until the transaction ends, so
// concurrent transitions of one task run one after another.
func (r *attemptRepo) LockAttemptState(ctx context.Context, taskID uint64) (*task.AttemptState, error) {
	if !hasTx(ctx) {
		return nil, errAttemptNeedsTx
	}
	row, err := r.q(ctx).LockAttemptState(ctx, taskID)
	if err != nil {
		return nil, mapAttemptErr(err)
	}
	// The lock row and the get row have identical fields.
	return attemptStateFromRow(taskID, sqlc.GetAttemptStateRow(row)), nil
}

func (r *attemptRepo) SaveAttemptState(ctx context.Context, state *task.AttemptState) error {
	if !hasTx(ctx) {
		return errAttemptNeedsTx
	}

	var checksumType, checksumValue, errorMessage sql.NullString
	if state.Checksum != nil {
		checksumType = sql.NullString{String: state.Checksum.ChecksumType, Valid: true}
		checksumValue = sql.NullString{String: state.Checksum.ChecksumValue, Valid: true}
	}
	if state.ErrorMessage != nil {
		errorMessage = sql.NullString{String: *state.ErrorMessage, Valid: true}
	}

	return r.q(ctx).SaveAttemptState(ctx, sqlc.SaveAttemptStateParams{
		Status:            string(state.Status),
		OwnerID:           sql.NullString{String: state.OwnerID, Valid: state.OwnerID != ""},
		LeaseExpiresAt:    nullTime(state.LeaseExpiresAt, time.Microsecond),
		AttemptGeneration: state.Generation,
		RetryCount:        uint32(state.RetryCount),
		NextAttemptAt:     nullTime(state.NextAttemptAt, time.Microsecond),
		FileName:          state.FileName,
		StorageType:       string(state.StorageType),
		StoragePath:       state.StoragePath,
		ChecksumType:      checksumType,
		ChecksumValue:     checksumValue,
		Progress:          sql.NullFloat64{Float64: state.Progress.Progress, Valid: true},
		DownloadedBytes:   sql.NullInt64{Int64: state.Progress.DownloadedBytes, Valid: true},
		TotalBytes:        sql.NullInt64{Int64: state.Progress.TotalBytes, Valid: true},
		ErrorMessage:      errorMessage,
		CompletedAt:       nullTime(state.CompletedAt, time.Second),
		ResultContentType: state.ContentType,
		ResultFileSize:    state.FileSize,
		ID:                state.TaskID,
	})
}

func mapAttemptErr(err error) error {
	if stderrors.Is(err, sql.ErrNoRows) {
		return errors.ErrNotFound
	}
	return err
}

// nullTime binds a nil time as NULL. Other times are truncated to the
// precision the column stores (DATETIME(6): microseconds, DATETIME: seconds),
// because MySQL rounds a longer fraction and could round up.
func nullTime(t *time.Time, precision time.Duration) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC().Truncate(precision), Valid: true}
}

// attemptStateFromRow maps one GetAttemptState row.
func attemptStateFromRow(taskID uint64, row sqlc.GetAttemptStateRow) *task.AttemptState {
	state := &task.AttemptState{
		TaskID:     taskID,
		Status:     task.TaskStatus(row.Status),
		OwnerID:    row.OwnerID.String, // NULL reads as ""
		Generation: row.AttemptGeneration,
		RetryCount: int(row.RetryCount),
		MaxRetries: int(row.MaxRetries),
		CreatedAt:  row.CreatedAt.Time,
		FileName:   row.FileName,
		// Kept exactly as stored (no normalization), so a save writes the
		// same value back.
		StorageType: storage.Type(row.StorageType),
		StoragePath: row.StoragePath,
		ContentType: row.ResultContentType,
		FileSize:    row.ResultFileSize,
		Progress: task.DownloadProgress{
			Progress:        row.Progress.Float64,
			DownloadedBytes: row.DownloadedBytes.Int64,
			TotalBytes:      row.TotalBytes.Int64,
		},
	}
	if row.LeaseExpiresAt.Valid {
		state.LeaseExpiresAt = &row.LeaseExpiresAt.Time
	}
	if row.NextAttemptAt.Valid {
		state.NextAttemptAt = &row.NextAttemptAt.Time
	}
	if row.ChecksumType.Valid || row.ChecksumValue.Valid {
		state.Checksum = &task.ChecksumInfo{
			ChecksumType:  row.ChecksumType.String,
			ChecksumValue: row.ChecksumValue.String,
		}
	}
	if row.ErrorMessage.Valid {
		state.ErrorMessage = &row.ErrorMessage.String
	}
	if row.CompletedAt.Valid {
		state.CompletedAt = &row.CompletedAt.Time
	}
	return state
}
