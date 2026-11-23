package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
	task "github.com/yuisofull/goload/internal/task"
)

func TestMigration0004_TaskOwnershipIsIdempotentAndComplete(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "mysql", "0004.task_ownership.sql"))
	require.NoError(t, err)
	ddl := strings.Join(strings.Fields(string(raw)), " ")

	// deployments/run-migrations.sh re-applies every file on each start, and
	// MySQL has no ADD COLUMN IF NOT EXISTS: every column is added through a
	// prepared statement guarded by an information_schema lookup.
	for _, column := range []struct{ name, definition string }{
		{"owner_id", "VARCHAR(64) NULL DEFAULT NULL"},
		{"lease_expires_at", "DATETIME(6) NULL DEFAULT NULL"},
		{"attempt_generation", "BIGINT UNSIGNED NOT NULL DEFAULT 0"},
		{"retry_count", "INT UNSIGNED NOT NULL DEFAULT 0"},
		{"next_attempt_at", "DATETIME(6) NULL DEFAULT NULL"},
	} {
		require.Contains(t, ddl, "AND column_name = '"+column.name+"'")
		require.Contains(t, ddl, "'ALTER TABLE tasks ADD COLUMN "+column.name+" "+column.definition+"'")
	}
	require.Equal(t, 5, strings.Count(ddl, "FROM information_schema.columns"))
	require.Equal(t, 5, strings.Count(ddl, "PREPARE stmt FROM @sqlstmt;"))
	require.Equal(t, 5, strings.Count(ddl, "EXECUTE stmt;"))
	require.Equal(t, 5, strings.Count(ddl, "DEALLOCATE PREPARE stmt;"))
	require.Equal(t, strings.Count(ddl, "ALTER TABLE"), strings.Count(ddl, "'ALTER TABLE"),
		"every ALTER TABLE must run through a guarded prepared statement")
	require.NotContains(t, strings.ToUpper(ddl), "DROP ")
	require.NotContains(t, string(raw), strings.Repeat("-", 2), "comments in this file start with #")
}

// attemptStateColumnNames is the column list both attempt-state SELECTs use.
var attemptStateColumnNames = []string{
	"status", "owner_id", "lease_expires_at", "attempt_generation", "retry_count", "next_attempt_at",
	"max_retries", "file_name", "storage_type", "storage_path", "checksum_type", "checksum_value",
	"progress", "downloaded_bytes", "total_bytes", "error_message", "completed_at",
	"result_content_type", "result_file_size", "created_at",
}

// attemptLease is already truncated to microseconds, as DATETIME(6) stores it.
var attemptLease = time.Date(2026, 1, 2, 3, 5, 5, 123456000, time.UTC)
var attemptCreated = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// openAttemptFakeDB is openFakeDB primed with attempt-state rows.
func openAttemptFakeDB(t *testing.T, rows ...[]driver.Value) (*fakeDB, *sql.DB) {
	t.Helper()
	fake, db := openFakeDB(t)
	fake.columns = attemptStateColumnNames
	fake.rows = rows
	return fake, db
}

// ownedAttemptRow is a task in STORING, held by worker-a as generation 4.
func ownedAttemptRow() []driver.Value {
	return []driver.Value{
		"STORING", "worker-a", attemptLease, int64(4), int64(1), nil,
		int64(3), "file.bin", "minio", "42/4/file-0011223344556677.bin", "md5", "abc123",
		float64(55.5), int64(555), int64(1000), "previous failure", nil,
		"application/zip", int64(2048), attemptCreated,
	}
}

// ownedAttemptState is ownedAttemptRow as the repository must map it.
func ownedAttemptState() *task.AttemptState {
	lease := attemptLease
	message := "previous failure"
	return &task.AttemptState{
		TaskID:         42,
		Status:         task.StatusStoring,
		OwnerID:        "worker-a",
		LeaseExpiresAt: &lease,
		Generation:     4,
		RetryCount:     1,
		MaxRetries:     3,
		FileName:       "file.bin",
		StorageType:    storage.TypeMinio,
		StoragePath:    "42/4/file-0011223344556677.bin",
		Checksum:       &task.ChecksumInfo{ChecksumType: "md5", ChecksumValue: "abc123"},
		Progress:       task.DownloadProgress{Progress: 55.5, DownloadedBytes: 555, TotalBytes: 1000},
		ErrorMessage:   &message,
		ContentType:    "application/zip",
		FileSize:       2048,
		CreatedAt:      attemptCreated,
	}
}

func TestAttemptRepo_LockAttemptState_SelectsForUpdateInsideTx(t *testing.T) {
	fake, db := openAttemptFakeDB(t, ownedAttemptRow())
	attempts := NewAttemptRepo(db)

	var got *task.AttemptState
	require.NoError(t, NewTxManager(db).DoInTx(context.Background(), func(ctx context.Context) error {
		var err error
		got, err = attempts.LockAttemptState(ctx, 42)
		return err
	}))

	require.Equal(t, ownedAttemptState(), got)
	steps, calls := fake.snapshot()
	require.Equal(t, []string{"begin", "query", "commit"}, steps)
	require.Len(t, calls, 1)
	require.True(t, calls[0].inTx, "the lock must run on the transaction from ctx")
	require.Contains(t, calls[0].query, "FROM tasks")
	require.Contains(t, calls[0].query, "WHERE id = ?")
	require.True(t, strings.HasSuffix(strings.TrimSpace(calls[0].query), "FOR UPDATE"))
	require.Equal(t, []driver.Value{int64(42)}, calls[0].args)
}

func TestAttemptRepo_LockAttemptState_NullColumnsMapToZeroValues(t *testing.T) {
	_, db := openAttemptFakeDB(t, []driver.Value{
		"PENDING", nil, nil, int64(0), int64(0), nil,
		int64(3), "file.bin", "", "", nil, nil,
		nil, nil, nil, nil, nil,
		"", int64(0), attemptCreated,
	})
	attempts := NewAttemptRepo(db)

	var got *task.AttemptState
	require.NoError(t, NewTxManager(db).DoInTx(context.Background(), func(ctx context.Context) error {
		var err error
		got, err = attempts.LockAttemptState(ctx, 42)
		return err
	}))

	require.Equal(t, &task.AttemptState{
		TaskID:     42,
		Status:     task.StatusPending,
		MaxRetries: 3,
		FileName:   "file.bin",
		CreatedAt:  attemptCreated,
	}, got)
}

func TestAttemptRepo_LockAttemptState_NotFound(t *testing.T) {
	_, db := openAttemptFakeDB(t) // the query returns no rows
	attempts := NewAttemptRepo(db)

	err := NewTxManager(db).DoInTx(context.Background(), func(ctx context.Context) error {
		_, err := attempts.LockAttemptState(ctx, 999)
		return err
	})

	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestAttemptRepo_LockAndSaveRequireTransaction(t *testing.T) {
	fake, db := openAttemptFakeDB(t, ownedAttemptRow())
	attempts := NewAttemptRepo(db)
	ctx := context.Background()

	locked, err := attempts.LockAttemptState(ctx, 42)
	require.ErrorContains(t, err, "inside TxManager.DoInTx")
	require.Nil(t, locked)

	err = attempts.SaveAttemptState(ctx, ownedAttemptState())
	require.ErrorContains(t, err, "inside TxManager.DoInTx")

	steps, _ := fake.snapshot()
	require.Empty(t, steps, "nothing may reach the database without a transaction")
}

func TestAttemptRepo_GetAttemptState_ReadsWithoutLock(t *testing.T) {
	completedAt := time.Date(2026, 1, 2, 3, 7, 5, 0, time.UTC)
	fake, db := openAttemptFakeDB(t, []driver.Value{
		"COMPLETED", nil, nil, int64(4), int64(1), nil,
		int64(3), "file.bin", "minio", "42/4/file-0011223344556677.bin", "md5", "abc123",
		float64(100), int64(1000), int64(1000), nil, completedAt,
		"text/plain", int64(1000), attemptCreated,
	})

	got, err := NewAttemptRepo(db).GetAttemptState(context.Background(), 42)

	require.NoError(t, err)
	require.Equal(t, &task.AttemptState{
		TaskID:      42,
		Status:      task.StatusCompleted,
		Generation:  4,
		RetryCount:  1,
		MaxRetries:  3,
		FileName:    "file.bin",
		StorageType: storage.TypeMinio,
		StoragePath: "42/4/file-0011223344556677.bin",
		Checksum:    &task.ChecksumInfo{ChecksumType: "md5", ChecksumValue: "abc123"},
		Progress:    task.DownloadProgress{Progress: 100, DownloadedBytes: 1000, TotalBytes: 1000},
		CompletedAt: &completedAt,
		ContentType: "text/plain",
		FileSize:    1000,
		CreatedAt:   attemptCreated,
	}, got)
	steps, calls := fake.snapshot()
	require.Equal(t, []string{"query"}, steps)
	require.False(t, calls[0].inTx)
	require.NotContains(t, calls[0].query, "FOR UPDATE")
	require.Equal(t, []driver.Value{int64(42)}, calls[0].args)
}

func TestAttemptRepo_SaveAttemptState_BindsEveryColumn(t *testing.T) {
	fake, db := openAttemptFakeDB(t)
	attempts := NewAttemptRepo(db)
	state := ownedAttemptState()
	// DATETIME(6) keeps microseconds; the repository truncates so MySQL does
	// not round up. completed_at is DATETIME (whole seconds).
	lease := time.Date(2026, 1, 2, 3, 5, 5, 123456789, time.UTC)
	completedAt := time.Date(2026, 1, 2, 3, 7, 5, 900000000, time.UTC)
	state.LeaseExpiresAt = &lease
	state.NextAttemptAt = &lease
	state.CompletedAt = &completedAt

	require.NoError(t, NewTxManager(db).DoInTx(context.Background(), func(ctx context.Context) error {
		return attempts.SaveAttemptState(ctx, state)
	}))

	steps, calls := fake.snapshot()
	require.Equal(t, []string{"begin", "exec", "commit"}, steps)
	require.Len(t, calls, 1)
	require.True(t, calls[0].inTx, "the save must run on the transaction from ctx")
	require.Contains(t, calls[0].query, "UPDATE tasks")
	require.Contains(t, calls[0].query, "WHERE id = ?")
	require.NotContains(t, calls[0].query, "max_retries", "max_retries is read-only")
	require.Equal(t, []driver.Value{
		"STORING", "worker-a", attemptLease, int64(4), int64(1), attemptLease,
		"file.bin", "minio", "42/4/file-0011223344556677.bin", "md5", "abc123",
		float64(55.5), int64(555), int64(1000), "previous failure",
		time.Date(2026, 1, 2, 3, 7, 5, 0, time.UTC),
		"application/zip", int64(2048),
		int64(42),
	}, calls[0].args)
}

func TestAttemptRepo_SaveAttemptState_BindsNullsForClearedFields(t *testing.T) {
	fake, db := openAttemptFakeDB(t)
	attempts := NewAttemptRepo(db)
	state := &task.AttemptState{
		TaskID:     42,
		Status:     task.StatusPending,
		Generation: 4,
		RetryCount: 2,
		FileName:   "file.bin",
	}

	require.NoError(t, NewTxManager(db).DoInTx(context.Background(), func(ctx context.Context) error {
		return attempts.SaveAttemptState(ctx, state)
	}))

	_, calls := fake.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, []driver.Value{
		"PENDING", nil, nil, int64(4), int64(2), nil,
		"file.bin", "", "", nil, nil,
		float64(0), int64(0), int64(0), nil,
		nil,
		"", int64(0),
		int64(42),
	}, calls[0].args)
}
