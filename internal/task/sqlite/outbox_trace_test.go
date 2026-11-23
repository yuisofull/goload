package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/task"
)

// preTracingOutboxDDL is task_outbox as shipped before the trace_context column.
const preTracingOutboxDDL = `CREATE TABLE task_outbox (
    id TEXT PRIMARY KEY NOT NULL,
    aggregate_id INTEGER NOT NULL,
    destination TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    sent_at TEXT
);`

func TestOutboxRepo_TraceContextRoundTrip(t *testing.T) {
	repo := NewOutboxRepo(newTestPool(t))
	ctx := context.Background()

	ev := newOutboxEvent("evt-1", 1, outboxTestTime())
	ev.TraceContext = map[string]string{
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		"tracestate":  "vendor=x",
	}
	require.NoError(t, repo.InsertUnsent(ctx, ev))

	got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, ev.TraceContext, got[0].TraceContext)
}

func TestOutboxRepo_NoTraceContextReadsNil(t *testing.T) {
	repo := NewOutboxRepo(newTestPool(t))
	ctx := context.Background()

	require.NoError(t, repo.InsertUnsent(ctx, newOutboxEvent("evt-1", 1, outboxTestTime())))

	got, err := repo.FetchUnsent(ctx, task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Nil(t, got[0].TraceContext)
}

// A database created before tracing holds unsent rows without the column; the
// upgrade adds it (idempotently) and those rows are still fetched for dispatch.
func TestEnsureOutboxSchema_UpgradesPreTracingTableAndKeepsUnsentRows(t *testing.T) {
	pool, err := sqlitex.Open(filepath.Join(t.TempDir(), "pre_tracing.db"), 0, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	conn := pool.Get(context.Background())
	require.NotNil(t, conn)
	require.NoError(t, sqlitex.ExecuteTransient(conn, preTracingOutboxDDL, nil))
	require.NoError(t, sqlitex.Execute(conn,
		`INSERT INTO task_outbox (id, aggregate_id, destination, event_type, payload, created_at, sent_at)
VALUES (?, ?, ?, ?, ?, ?, NULL)`,
		&sqlitex.ExecOptions{Args: []any{
			"old-1", 9, task.OutboxDestinationDispatch, task.OutboxEventTaskCreated,
			[]byte(`{"task_id":9}`), outboxTestTime().UTC().Format(outboxTimeLayout),
		}},
	))
	require.NoError(t, EnsureOutboxSchema(conn))
	require.NoError(t, EnsureOutboxSchema(conn), "the upgrade must be idempotent")
	pool.Put(conn)

	got, err := NewOutboxRepo(pool).FetchUnsent(context.Background(), task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "old-1", got[0].ID)
	require.Nil(t, got[0].TraceContext, "a pre-tracing row has no trace context")
}

func TestOutboxRepo_LatestTraceContextIsTheNewestRowOfTheTaskAndType(t *testing.T) {
	repo := NewOutboxRepo(newTestPool(t))
	ctx := context.Background()
	at := outboxTestTime()

	older := newOutboxEvent("evt-older", 1, at)
	older.TraceContext = map[string]string{"traceparent": "00-11111111111111111111111111111111-1111111111111111-01"}
	newer := newOutboxEvent("evt-newer", 1, at.Add(time.Second))
	newer.TraceContext = map[string]string{"traceparent": "00-22222222222222222222222222222222-2222222222222222-01"}
	otherTask := newOutboxEvent("evt-other", 2, at.Add(2*time.Second))
	otherTask.TraceContext = map[string]string{"traceparent": "00-33333333333333333333333333333333-3333333333333333-01"}
	for _, ev := range []task.OutboxEvent{older, newer, otherTask} {
		require.NoError(t, repo.InsertUnsent(ctx, ev))
	}

	got, err := repo.LatestTraceContext(ctx, 1, task.OutboxEventTaskCreated)
	require.NoError(t, err)
	require.Equal(t, newer.TraceContext, got)

	none, err := repo.LatestTraceContext(ctx, 99, task.OutboxEventTaskCreated)
	require.NoError(t, err)
	require.Nil(t, none)
}
