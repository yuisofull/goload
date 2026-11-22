package sqlite

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/stretchr/testify/require"

	task "github.com/yuisofull/goload/internal/task"
)

// outboxTestTime has nanoseconds set to prove full precision round-trips.
func outboxTestTime() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
}

func newOutboxEvent(id string, taskID uint64, createdAt time.Time) task.OutboxEvent {
	return task.OutboxEvent{
		ID:          id,
		AggregateID: taskID,
		Destination: task.OutboxDestinationDispatch,
		EventType:   task.OutboxEventTaskCreated,
		Payload:     []byte(`{"task_id":` + strconv.FormatUint(taskID, 10) + `}`),
		CreatedAt:   createdAt,
	}
}

func outboxIDs(events []task.OutboxEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

// pendingTask is a task row as CreateTask builds it. DownloadOptions must be
// set: the SQLite repo binds max_retries explicitly and the column is NOT NULL.
func pendingTask(accountID uint64) *task.Task {
	return &task.Task{
		OfAccountID:     accountID,
		FileName:        "file.bin",
		SourceURL:       "https://example.test/file.bin",
		SourceType:      task.SourceHTTPS,
		DownloadOptions: &task.DownloadOptions{Concurrency: 16, MaxRetries: 3},
		Status:          task.StatusPending,
	}
}

// outboxSentAt reads the raw sent_at column; ok is false while it is NULL.
func outboxSentAt(t *testing.T, pool *sqlitex.Pool, id string) (string, bool) {
	t.Helper()

	conn := pool.Get(context.Background())
	require.NotNil(t, conn)
	defer pool.Put(conn)

	var (
		value string
		ok    bool
	)
	require.NoError(t, sqlitex.Execute(conn, `SELECT sent_at FROM task_outbox WHERE id = ?`, &sqlitex.ExecOptions{
		Args: []any{id},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			if stmt.ColumnType(0) != sqlite.SQLITE_NULL {
				value, ok = stmt.ColumnText(0), true
			}
			return nil
		},
	}))
	return value, ok
}

func TestOutboxRepo_InsertUnsentThenFetchUnsent(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()

	want := newOutboxEvent("evt-1", 42, outboxTestTime())
	require.NoError(t, repo.InsertUnsent(ctx, want))

	got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, want.ID, got[0].ID)
	require.Equal(t, want.AggregateID, got[0].AggregateID)
	require.Equal(t, want.Destination, got[0].Destination)
	require.Equal(t, want.EventType, got[0].EventType)
	require.Equal(t, want.Payload, got[0].Payload)
	require.True(t, want.CreatedAt.Equal(got[0].CreatedAt), "created_at must round-trip with nanoseconds")
	require.Nil(t, got[0].SentAt)
}

func TestOutboxRepo_OutboxBacklog_CountsUnsentAndOldest(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()
	base := outboxTestTime()

	// Oldest unsent, then two newer; one of the newer ones is marked sent.
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-old", 1, base)))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-mid", 2, base.Add(time.Second))))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-new", 3, base.Add(2*time.Second))))
	require.NoError(t, repo.MarkSent(ctx, "evt-mid"))

	count, oldest, err := repo.OutboxBacklog(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), count, "two events remain unsent")
	require.True(t, oldest.Equal(base), "oldest unsent is evt-old, not a page artifact")
}

func TestOutboxRepo_OutboxBacklog_EmptyIsZero(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)

	count, oldest, err := repo.OutboxBacklog(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
	require.True(t, oldest.IsZero())
}

func TestOutboxRepo_MarkSentRemovesFromFetchUnsent(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()

	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-1", 1, outboxTestTime())))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-2", 2, outboxTestTime().Add(time.Millisecond))))

	require.NoError(t, repo.MarkSent(ctx, "evt-1"))

	got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"evt-2"}, outboxIDs(got))

	sentAt, ok := outboxSentAt(t, pool, "evt-1")
	require.True(t, ok, "sent_at must be set for the marked event")
	_, err = time.Parse(outboxTimeLayout, sentAt)
	require.NoError(t, err)

	_, ok = outboxSentAt(t, pool, "evt-2")
	require.False(t, ok, "sent_at must stay NULL for the other event")
}

func TestOutboxRepo_MarkSentIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()

	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-1", 1, outboxTestTime())))
	require.NoError(t, repo.MarkSent(ctx, "evt-1"))
	first, ok := outboxSentAt(t, pool, "evt-1")
	require.True(t, ok)

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, repo.MarkSent(ctx, "evt-1"))
	second, ok := outboxSentAt(t, pool, "evt-1")
	require.True(t, ok)
	require.Equal(t, first, second, "a second MarkSent must not overwrite sent_at")

	require.NoError(t, repo.MarkSent(ctx, "no-such-event"))
}

func TestOutboxRepo_FetchUnsent_OldestFirstAndLimited(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()
	base := outboxTestTime()

	// Inserted out of chronological order on purpose.
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-c", 3, base.Add(2*time.Millisecond))))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-a", 1, base)))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-b", 2, base.Add(time.Millisecond))))

	all, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"evt-a", "evt-b", "evt-c"}, outboxIDs(all))

	limited, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 2)
	require.NoError(t, err)
	require.Equal(t, []string{"evt-a", "evt-b"}, outboxIDs(limited))
}

func TestOutboxRepo_FetchUnsent_SameTimestampOrdersByID(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()
	at := outboxTestTime()

	// Same created_at: id order breaks the tie, not insertion order.
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-z", 1, at)))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-y", 2, at)))

	got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"evt-y", "evt-z"}, outboxIDs(got))
}

func TestOutboxRepo_FetchUnsent_NonPositiveLimit(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-1", 1, outboxTestTime())))

	// SQLite treats a negative LIMIT as "no limit"; the repo must not.
	for _, limit := range []int{0, -1} {
		got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, limit)
		require.NoError(t, err)
		require.Empty(t, got)
	}
}

func TestOutboxRepo_InsertUnsent_DuplicateIDFails(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()

	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-dup", 1, outboxTestTime())))
	require.Error(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-dup", 2, outboxTestTime())))

	got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint64(1), got[0].AggregateID)
}

func TestOutboxRepo_LargePayloadRoundTrips(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()

	// 200 KiB, well past the 64 KiB limit of a plain MySQL BLOB.
	payload := make([]byte, 200*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	event := newOutboxEvent("evt-big", 1, outboxTestTime())
	event.Payload = payload
	require.NoError(t, repo.InsertUnsent(ctx, event))

	got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.True(t, bytes.Equal(payload, got[0].Payload), "payload must round-trip byte for byte")
}

func TestDoInTx_RollsBackTaskAndOutboxTogether(t *testing.T) {
	pool := newTestPool(t)
	repo := NewTaskRepo(pool)
	outbox := NewOutboxRepo(pool)
	tx := NewTxManager(pool)
	ctx := context.Background()

	boom := errors.New("boom after create")
	err := tx.DoInTx(ctx, func(ctx context.Context) error {
		created, err := repo.Create(ctx, pendingTask(7))
		if err != nil {
			return err
		}
		event, err := task.NewTaskCreatedOutboxEvent(created)
		if err != nil {
			return err
		}
		if err := outbox.InsertUnsent(ctx, event); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	count, err := repo.GetTaskCountOfAccount(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, count, "the task row must not persist")

	unsent, err := outbox.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Empty(t, unsent, "the outbox row must not persist")
}

func TestDoInTx_CommitsTaskAndOutboxTogether(t *testing.T) {
	pool := newTestPool(t)
	repo := NewTaskRepo(pool)
	outbox := NewOutboxRepo(pool)
	tx := NewTxManager(pool)
	ctx := context.Background()

	var createdID uint64
	require.NoError(t, tx.DoInTx(ctx, func(ctx context.Context) error {
		created, err := repo.Create(ctx, pendingTask(7))
		if err != nil {
			return err
		}
		createdID = created.ID
		event, err := task.NewTaskCreatedOutboxEvent(created)
		if err != nil {
			return err
		}
		return outbox.InsertUnsent(ctx, event)
	}))

	count, err := repo.GetTaskCountOfAccount(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)

	unsent, err := outbox.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Len(t, unsent, 1)
	require.Equal(t, createdID, unsent[0].AggregateID)
	require.Equal(t, task.OutboxEventTaskCreated, unsent[0].EventType)
	require.Equal(t, task.OutboxDestinationDispatch, unsent[0].Destination)
}

func TestDoInTx_DuplicateOutboxIDRollsBackTask(t *testing.T) {
	pool := newTestPool(t)
	repo := NewTaskRepo(pool)
	outbox := NewOutboxRepo(pool)
	tx := NewTxManager(pool)
	ctx := context.Background()

	require.NoError(t, outbox.InsertUnsent(ctx, newOutboxEvent("evt-dup", 1, outboxTestTime())))

	err := tx.DoInTx(ctx, func(ctx context.Context) error {
		if _, err := repo.Create(ctx, pendingTask(7)); err != nil {
			return err
		}
		return outbox.InsertUnsent(ctx, newOutboxEvent("evt-dup", 2, outboxTestTime()))
	})
	require.Error(t, err)

	count, err := repo.GetTaskCountOfAccount(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, count, "the task row must roll back with the failed outbox insert")
}

func TestOutboxRepo_FetchUnsent_CursorPagesWithoutRepeats(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOutboxRepo(pool)
	ctx := context.Background()
	base := outboxTestTime()

	// Includes a created_at tie broken by id, inserted out of order.
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-e", 5, base.Add(3*time.Millisecond))))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-b", 2, base.Add(time.Millisecond))))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-a", 1, base)))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-d", 4, base.Add(time.Millisecond))))
	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-c", 3, base.Add(2*time.Millisecond))))

	var (
		seen   []string
		cursor task.OutboxCursor
	)
	for {
		page, err := repo.FetchUnsent(ctx, cursor, 2)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		seen = append(seen, outboxIDs(page)...)
		last := page[len(page)-1]
		cursor = task.OutboxCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	require.Equal(t, []string{"evt-a", "evt-b", "evt-d", "evt-c", "evt-e"}, seen)
}
