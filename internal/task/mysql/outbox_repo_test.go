package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	task "github.com/yuisofull/goload/internal/task"
)

// fakeDB is a minimal database/sql driver that records what the outbox
// repository sends, so SQL text, arguments and transaction boundaries can be
// asserted without a MySQL server.
type fakeDB struct {
	mu      sync.Mutex
	steps   []string         // "begin", "exec", "query", "commit", "rollback"
	calls   []fakeCall       // every exec and query, in order
	rows    [][]driver.Value // rows handed to the next query
	columns []string         // column names of rows; nil means the task_outbox columns
}

type fakeCall struct {
	kind  string // "exec" or "query"
	query string
	args  []driver.Value
	inTx  bool
}

func (f *fakeDB) step(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, name)
}

func (f *fakeDB) record(kind, query string, named []driver.NamedValue, inTx bool) {
	args := make([]driver.Value, len(named))
	for i, nv := range named {
		args[i] = nv.Value
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, kind)
	f.calls = append(f.calls, fakeCall{kind: kind, query: query, args: args, inTx: inTx})
}

func (f *fakeDB) takeRows() [][]driver.Value {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.rows
	f.rows = nil
	return rows
}

func (f *fakeDB) rowColumns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.columns
}

func (f *fakeDB) snapshot() ([]string, []fakeCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.steps...), append([]fakeCall(nil), f.calls...)
}

// Connect and Driver make fakeDB a driver.Connector for sql.OpenDB.
func (f *fakeDB) Connect(context.Context) (driver.Conn, error) { return &fakeConn{db: f}, nil }
func (f *fakeDB) Driver() driver.Driver                        { return fakeDriver{db: f} }

type fakeDriver struct{ db *fakeDB }

func (d fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{db: d.db}, nil }

type fakeConn struct {
	db   *fakeDB
	inTx bool
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fakeConn: Prepare is not supported")
}

func (c *fakeConn) Close() error { return nil }

func (c *fakeConn) Begin() (driver.Tx, error) {
	c.inTx = true
	c.db.step("begin")
	return &fakeTx{conn: c}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.db.record("exec", query, args, c.inTx)
	return driver.RowsAffected(1), nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.db.record("query", query, args, c.inTx)
	return &fakeRows{columns: c.db.rowColumns(), rows: c.db.takeRows()}, nil
}

type fakeTx struct{ conn *fakeConn }

func (t *fakeTx) Commit() error {
	t.conn.inTx = false
	t.conn.db.step("commit")
	return nil
}

func (t *fakeTx) Rollback() error {
	t.conn.inTx = false
	t.conn.db.step("rollback")
	return nil
}

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	next    int
}

func (r *fakeRows) Columns() []string {
	if r.columns != nil {
		return r.columns
	}
	// The outbox fetch queries are SELECT *, so the row carries every column,
	// sent_at and trace_context included.
	return []string{"id", "aggregate_id", "destination", "event_type", "payload", "created_at", "sent_at", "trace_context"}
}

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}

func openFakeDB(t *testing.T) (*fakeDB, *sql.DB) {
	t.Helper()
	fake := &fakeDB{}
	db := sql.OpenDB(fake)
	t.Cleanup(func() { _ = db.Close() })
	return fake, db
}

func sampleOutboxEvent() task.OutboxEvent {
	return task.OutboxEvent{
		ID:          "evt-1",
		AggregateID: 42,
		Destination: task.OutboxDestinationDispatch,
		EventType:   task.OutboxEventTaskCreated,
		Payload:     []byte(`{"task_id":42}`),
		CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC),
	}
}

func requireInsertCall(t *testing.T, call fakeCall) {
	t.Helper()
	require.Equal(t, "exec", call.kind)
	require.Contains(t, call.query, "INSERT INTO task_outbox")
	require.Len(t, call.args, 7)
	require.Equal(t, "evt-1", call.args[0])
	require.Equal(t, int64(42), call.args[1])
	require.Equal(t, "dispatch", call.args[2])
	require.Equal(t, "TaskCreated", call.args[3])
	require.Equal(t, []byte(`{"task_id":42}`), call.args[4])
	createdAt, ok := call.args[5].(time.Time)
	require.True(t, ok, "created_at must be bound as time.Time")
	require.True(t,
		createdAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)),
		"created_at must be truncated to microseconds (DATETIME(6))",
	)
	require.Nil(t, call.args[6], "an event without trace context binds trace_context as NULL")
}

func TestOutboxRepo_InsertUnsent_JoinsCallerTx(t *testing.T) {
	fake, db := openFakeDB(t)
	repo := NewOutboxRepo(db)
	tx := NewTxManager(db)

	require.NoError(t, tx.DoInTx(context.Background(), func(ctx context.Context) error {
		return repo.InsertUnsent(ctx, sampleOutboxEvent())
	}))

	steps, calls := fake.snapshot()
	require.Equal(t, []string{"begin", "exec", "commit"}, steps)
	require.Len(t, calls, 1)
	require.True(t, calls[0].inTx, "the insert must run on the transaction from ctx")
	requireInsertCall(t, calls[0])
}

func TestOutboxRepo_InsertUnsent_RollsBackWithCallerTx(t *testing.T) {
	fake, db := openFakeDB(t)
	repo := NewOutboxRepo(db)
	tx := NewTxManager(db)

	boom := errors.New("boom after insert")
	err := tx.DoInTx(context.Background(), func(ctx context.Context) error {
		if err := repo.InsertUnsent(ctx, sampleOutboxEvent()); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	steps, calls := fake.snapshot()
	require.Equal(t, []string{"begin", "exec", "rollback"}, steps)
	require.Len(t, calls, 1)
	require.True(t, calls[0].inTx)
}

func TestOutboxRepo_InsertUnsent_WithoutTxUsesDB(t *testing.T) {
	fake, db := openFakeDB(t)

	require.NoError(t, NewOutboxRepo(db).InsertUnsent(context.Background(), sampleOutboxEvent()))

	steps, calls := fake.snapshot()
	require.Equal(t, []string{"exec"}, steps)
	require.Len(t, calls, 1)
	require.False(t, calls[0].inTx)
	requireInsertCall(t, calls[0])
}

func TestOutboxRepo_FetchUnsent_MapsRowsAndPassesLimit(t *testing.T) {
	fake, db := openFakeDB(t)
	created := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	fake.rows = [][]driver.Value{
		{"evt-1", int64(42), "dispatch", "TaskCreated", []byte(`{"task_id":42}`), created, nil,
			`{"traceparent":"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}`},
		{"evt-2", int64(43), "dispatch", "TaskCreated", []byte(`{"task_id":43}`), created.Add(time.Second), nil, nil},
	}

	events, err := NewOutboxRepo(db).FetchUnsent(context.Background(), task.OutboxCursor{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "evt-1", events[0].ID)
	require.Equal(t, uint64(42), events[0].AggregateID)
	require.Equal(t, task.OutboxDestinationDispatch, events[0].Destination)
	require.Equal(t, task.OutboxEventTaskCreated, events[0].EventType)
	require.Equal(t, `{"task_id":42}`, string(events[0].Payload))
	require.True(t, created.Equal(events[0].CreatedAt))
	require.Nil(t, events[0].SentAt)
	require.Equal(t,
		map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		events[0].TraceContext)
	require.Equal(t, "evt-2", events[1].ID)
	require.Equal(t, uint64(43), events[1].AggregateID)
	require.Nil(t, events[1].TraceContext, "a NULL trace_context reads as nil")

	_, calls := fake.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "query", calls[0].kind)
	require.False(t, calls[0].inTx)
	require.Contains(t, calls[0].query, "WHERE sent_at IS NULL")
	require.Contains(t, calls[0].query, "ORDER BY created_at ASC, id ASC")
	require.Equal(t, []driver.Value{int64(10)}, calls[0].args)
}

func TestOutboxRepo_FetchUnsent_NonPositiveLimitSkipsQuery(t *testing.T) {
	fake, db := openFakeDB(t)
	repo := NewOutboxRepo(db)

	// MySQL rejects a negative LIMIT; the repo answers without asking.
	for _, limit := range []int{0, -1} {
		events, err := repo.FetchUnsent(context.Background(), task.OutboxCursor{}, limit)
		require.NoError(t, err)
		require.Empty(t, events)
	}

	steps, _ := fake.snapshot()
	require.Empty(t, steps)
}

func TestOutboxRepo_MarkSent_OnlyTouchesUnsentRow(t *testing.T) {
	fake, db := openFakeDB(t)

	before := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, NewOutboxRepo(db).MarkSent(context.Background(), "evt-1"))

	steps, calls := fake.snapshot()
	require.Equal(t, []string{"exec"}, steps)
	require.Len(t, calls, 1)
	require.Contains(t, calls[0].query, "UPDATE task_outbox")
	require.Contains(t, calls[0].query, "SET sent_at = ?")
	require.Contains(t, calls[0].query, "WHERE id = ? AND sent_at IS NULL")
	require.Len(t, calls[0].args, 2)
	sentAt, ok := calls[0].args[0].(time.Time)
	require.True(t, ok, "sent_at must be bound as time.Time")
	require.False(t, sentAt.Before(before))
	require.Equal(t, "evt-1", calls[0].args[1])
}

func TestMigration0003_TaskOutboxIsIdempotentAndComplete(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "mysql", "0003.task_outbox.sql"))
	require.NoError(t, err)
	ddl := strings.Join(strings.Fields(string(raw)), " ")

	// deployments/run-migrations.sh re-applies every file on each start.
	require.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS task_outbox (")
	require.NotContains(t, strings.ToUpper(ddl), "DROP TABLE")

	for _, fragment := range []string{
		"id CHAR(36) NOT NULL",
		"aggregate_id BIGINT UNSIGNED NOT NULL",
		"destination VARCHAR(32) NOT NULL",
		"event_type VARCHAR(64) NOT NULL",
		"payload MEDIUMBLOB NOT NULL",
		"created_at DATETIME(6) NOT NULL",
		"sent_at DATETIME(6) NULL DEFAULT NULL",
		"PRIMARY KEY (id)",
		"INDEX task_outbox_unsent_idx (sent_at, created_at)",
		"INDEX task_outbox_aggregate_idx (aggregate_id)",
	} {
		require.Contains(t, ddl, fragment)
	}
}

func TestOutboxRepo_FetchUnsent_CursorUsesKeysetPredicate(t *testing.T) {
	fake, db := openFakeDB(t)
	created := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	fake.rows = [][]driver.Value{
		{"evt-3", int64(43), "dispatch", "TaskCreated", []byte(`{}`), created.Add(time.Second), nil, nil},
	}

	cursor := task.OutboxCursor{CreatedAt: created, ID: "evt-2"}
	events, err := NewOutboxRepo(db).FetchUnsent(context.Background(), cursor, 5)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "evt-3", events[0].ID)

	_, calls := fake.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "query", calls[0].kind)
	require.Contains(t, calls[0].query, "(created_at > ? OR (created_at = ? AND id > ?))")
	require.Contains(t, calls[0].query, "ORDER BY created_at ASC, id ASC")
	require.Equal(t, []driver.Value{created, created, "evt-2", int64(5)}, calls[0].args)
}

func TestOutboxRepo_InsertUnsent_BindsTheTraceContextAsJSON(t *testing.T) {
	fake, db := openFakeDB(t)
	ev := sampleOutboxEvent()
	ev.TraceContext = map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}

	require.NoError(t, NewOutboxRepo(db).InsertUnsent(context.Background(), ev))

	_, calls := fake.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, `{"traceparent":"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}`, calls[0].args[6])
}

func TestOutboxRepo_LatestTraceContext_ReadsTheNewestRowOfTheTaskAndType(t *testing.T) {
	fake, db := openFakeDB(t)
	fake.columns = []string{"trace_context"}
	fake.rows = [][]driver.Value{{`{"traceparent":"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}`}}

	got, err := NewOutboxRepo(db).LatestTraceContext(context.Background(), 42, task.OutboxEventTaskCreated)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}, got)

	_, calls := fake.snapshot()
	require.Len(t, calls, 1)
	require.Contains(t, calls[0].query, "ORDER BY created_at DESC, id DESC")
	require.Equal(t, []driver.Value{int64(42), "TaskCreated"}, calls[0].args)
}

func TestOutboxRepo_LatestTraceContext_NoRowIsNil(t *testing.T) {
	fake, db := openFakeDB(t)
	fake.columns = []string{"trace_context"}

	got, err := NewOutboxRepo(db).LatestTraceContext(context.Background(), 42, task.OutboxEventTaskCreated)
	require.NoError(t, err)
	require.Nil(t, got)
}
