package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/stretchr/testify/require"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
	task "github.com/yuisofull/goload/internal/task"
)

// seedTask stores a fresh PENDING task (max_retries 3) and returns its id.
func seedTask(t *testing.T, pool *sqlitex.Pool) uint64 {
	t.Helper()

	created, err := NewTaskRepo(pool).Create(context.Background(), pendingTask(7))
	require.NoError(t, err)
	return created.ID
}

// rawColumnIsNull reports whether a tasks column holds SQL NULL for the row.
func rawColumnIsNull(t *testing.T, pool *sqlitex.Pool, taskID uint64, column string) bool {
	t.Helper()

	conn := pool.Get(context.Background())
	require.NotNil(t, conn)
	defer pool.Put(conn)

	isNull := false
	require.NoError(t, sqlitex.ExecuteTransient(
		conn,
		`SELECT `+column+` FROM tasks WHERE id = ?;`,
		&sqlitex.ExecOptions{
			Args: []any{taskID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				isNull = stmt.ColumnType(0) == sqlite.SQLITE_NULL
				return nil
			},
		},
	))
	return isNull
}

func TestAttemptRepo_GetAttemptState_FreshTask(t *testing.T) {
	pool := newTestPool(t)
	id := seedTask(t, pool)

	got, err := NewAttemptRepo(pool).GetAttemptState(context.Background(), id)

	require.NoError(t, err)
	require.False(t, got.CreatedAt.IsZero(), "created_at is read from the row")
	got.CreatedAt = time.Time{} // read-only, DB-assigned; checked above
	require.Equal(t, &task.AttemptState{
		TaskID:     id,
		Status:     task.StatusPending,
		MaxRetries: 3,
		FileName:   "file.bin",
	}, got)
}

func TestAttemptRepo_GetAttemptState_NotFound(t *testing.T) {
	pool := newTestPool(t)

	got, err := NewAttemptRepo(pool).GetAttemptState(context.Background(), 999)

	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.Nil(t, got)
}

func TestAttemptRepo_LockAndSaveRoundTrip(t *testing.T) {
	pool := newTestPool(t)
	attempts := NewAttemptRepo(pool)
	tx := NewTxManager(pool)
	ctx := context.Background()
	id := seedTask(t, pool)

	// Nanoseconds are set to prove the lease keeps full precision.
	lease := time.Date(2026, 1, 2, 3, 5, 5, 123456789, time.UTC)
	nextAttempt := time.Date(2026, 1, 2, 3, 6, 5, 987654321, time.UTC)
	completedAt := time.Date(2026, 1, 2, 3, 7, 5, 0, time.UTC)
	message := "connection reset"
	want := &task.AttemptState{
		TaskID:         id,
		Status:         task.StatusStoring,
		OwnerID:        "worker-a",
		LeaseExpiresAt: &lease,
		Generation:     3,
		RetryCount:     2,
		NextAttemptAt:  &nextAttempt,
		MaxRetries:     3,
		FileName:       "resolved.bin",
		StorageType:    storage.TypeLocal,
		StoragePath:    "7/3/resolved-0011223344556677.bin",
		Checksum:       &task.ChecksumInfo{ChecksumType: "md5", ChecksumValue: "abc123"},
		Progress:       task.DownloadProgress{Progress: 55.5, DownloadedBytes: 555, TotalBytes: 1000},
		ErrorMessage:   &message,
		CompletedAt:    &completedAt,
	}

	require.NoError(t, tx.DoInTx(ctx, func(ctx context.Context) error {
		locked, err := attempts.LockAttemptState(ctx, id)
		if err != nil {
			return err
		}
		require.Equal(t, task.StatusPending, locked.Status)
		require.Equal(t, 3, locked.MaxRetries)

		toSave := *want
		toSave.MaxRetries = 99 // read-only: must not be written back
		return attempts.SaveAttemptState(ctx, &toSave)
	}))

	got, err := attempts.GetAttemptState(ctx, id)
	require.NoError(t, err)
	require.False(t, got.CreatedAt.IsZero(), "created_at is read from the row")
	want.CreatedAt = got.CreatedAt // read-only, DB-assigned; not part of the save round-trip
	require.Equal(t, want, got)
}

func TestAttemptRepo_SaveWritesNullsForClearedFields(t *testing.T) {
	pool := newTestPool(t)
	attempts := NewAttemptRepo(pool)
	tx := NewTxManager(pool)
	ctx := context.Background()
	id := seedTask(t, pool)

	nullable := []string{
		"owner_id", "lease_expires_at", "next_attempt_at", "error_message",
		"completed_at", "checksum_type", "checksum_value",
	}
	save := func(mutate func(st *task.AttemptState)) {
		t.Helper()
		require.NoError(t, tx.DoInTx(ctx, func(ctx context.Context) error {
			st, err := attempts.LockAttemptState(ctx, id)
			if err != nil {
				return err
			}
			mutate(st)
			return attempts.SaveAttemptState(ctx, st)
		}))
	}

	at := time.Date(2026, 1, 2, 3, 5, 5, 0, time.UTC)
	message := "boom"
	save(func(st *task.AttemptState) {
		st.OwnerID = "worker-a"
		st.LeaseExpiresAt = &at
		st.NextAttemptAt = &at
		st.ErrorMessage = &message
		st.CompletedAt = &at
		st.Checksum = &task.ChecksumInfo{ChecksumType: "md5", ChecksumValue: "abc123"}
	})
	for _, column := range nullable {
		require.False(t, rawColumnIsNull(t, pool, id, column), "%s must be set", column)
	}

	save(func(st *task.AttemptState) {
		st.OwnerID = ""
		st.LeaseExpiresAt = nil
		st.NextAttemptAt = nil
		st.ErrorMessage = nil
		st.CompletedAt = nil
		st.Checksum = nil
	})
	for _, column := range nullable {
		require.True(t, rawColumnIsNull(t, pool, id, column), "%s must be SQL NULL, not text", column)
	}
}

func TestAttemptRepo_LockAndSaveRequireTransaction(t *testing.T) {
	pool := newTestPool(t)
	attempts := NewAttemptRepo(pool)
	ctx := context.Background()
	id := seedTask(t, pool)

	locked, err := attempts.LockAttemptState(ctx, id)
	require.ErrorContains(t, err, "inside TxManager.DoInTx")
	require.Nil(t, locked)

	err = attempts.SaveAttemptState(ctx, &task.AttemptState{TaskID: id, Status: task.StatusCancelled})
	require.ErrorContains(t, err, "inside TxManager.DoInTx")

	got, err := attempts.GetAttemptState(ctx, id)
	require.NoError(t, err)
	require.Equal(t, task.StatusPending, got.Status, "a save outside a transaction must not write")
}

func TestAttemptRepo_LockAttemptState_NotFoundInsideTx(t *testing.T) {
	pool := newTestPool(t)
	attempts := NewAttemptRepo(pool)

	err := NewTxManager(pool).DoInTx(context.Background(), func(ctx context.Context) error {
		_, err := attempts.LockAttemptState(ctx, 999)
		return err
	})

	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestAttemptRepo_RollbackDiscardsSave(t *testing.T) {
	pool := newTestPool(t)
	attempts := NewAttemptRepo(pool)
	ctx := context.Background()
	id := seedTask(t, pool)

	boom := errors.New("boom after save")
	err := NewTxManager(pool).DoInTx(ctx, func(ctx context.Context) error {
		st, err := attempts.LockAttemptState(ctx, id)
		if err != nil {
			return err
		}
		st.Status = task.StatusCancelled
		st.Generation = 9
		if err := attempts.SaveAttemptState(ctx, st); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	got, err := attempts.GetAttemptState(ctx, id)
	require.NoError(t, err)
	require.Equal(t, task.StatusPending, got.Status)
	require.Zero(t, got.Generation)
}

func TestAttemptRepo_LockSerializesConcurrentTransactions(t *testing.T) {
	pool := newTestPool(t)
	attempts := NewAttemptRepo(pool)
	tx := NewTxManager(pool)
	id := seedTask(t, pool)

	// Every transaction reads the generation under the lock and writes it
	// back plus one. Without the lock two of them would read the same value
	// (a lost update) or fail with SQLITE_BUSY.
	const writers = 8
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = tx.DoInTx(context.Background(), func(ctx context.Context) error {
				st, err := attempts.LockAttemptState(ctx, id)
				if err != nil {
					return err
				}
				st.Generation++
				return attempts.SaveAttemptState(ctx, st)
			})
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	got, err := attempts.GetAttemptState(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, uint64(writers), got.Generation)
}
