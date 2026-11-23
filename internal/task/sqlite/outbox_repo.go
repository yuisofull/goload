package sqlite

import (
	"context"
	"fmt"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"

	task "github.com/yuisofull/goload/internal/task"
)

// outboxTimeLayout stores timestamps as fixed-width UTC text, so comparing
// the strings compares the times.
const outboxTimeLayout = "2006-01-02T15:04:05.000000000Z"

type outboxRepo struct {
	pool *sqlitex.Pool
}

// NewOutboxRepo returns the SQLite implementation of task.OutboxRepository.
func NewOutboxRepo(pool *sqlitex.Pool) task.OutboxRepository {
	return &outboxRepo{pool: pool}
}

// withConn runs fn on the connection DoInTx stored in ctx, or on a pooled
// connection when ctx carries none.
func (r *outboxRepo) withConn(ctx context.Context, fn func(conn *sqlite.Conn) error) error {
	if conn, ok := ctx.Value(connKey{}).(*sqlite.Conn); ok {
		return fn(conn)
	}
	conn := r.pool.Get(ctx)
	if conn == nil {
		return context.DeadlineExceeded
	}
	defer r.pool.Put(conn)
	return fn(conn)
}

func (r *outboxRepo) InsertUnsent(ctx context.Context, event task.OutboxEvent) error {
	return r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(
			conn,
			`INSERT INTO task_outbox (id, aggregate_id, destination, event_type, payload, created_at, sent_at, trace_context)
VALUES (?, ?, ?, ?, ?, ?, NULL, ?)`,
			&sqlitex.ExecOptions{
				Args: []any{
					event.ID, event.AggregateID, event.Destination, event.EventType, event.Payload,
					event.CreatedAt.UTC().Format(outboxTimeLayout), traceContextArg(event.TraceContext),
				},
			},
		)
	})
}

func (r *outboxRepo) FetchUnsent(ctx context.Context, after task.OutboxCursor, limit int) ([]task.OutboxEvent, error) {
	// SQLite reads a negative LIMIT as "no limit".
	if limit <= 0 {
		return nil, nil
	}

	query := `SELECT id, aggregate_id, destination, event_type, payload, created_at, trace_context
FROM task_outbox
WHERE sent_at IS NULL
ORDER BY created_at ASC, id ASC
LIMIT ?`
	args := []any{limit}
	if !after.CreatedAt.IsZero() {
		createdAt := after.CreatedAt.UTC().Format(outboxTimeLayout)
		query = `SELECT id, aggregate_id, destination, event_type, payload, created_at, trace_context
FROM task_outbox
WHERE sent_at IS NULL AND (created_at > ?1 OR (created_at = ?1 AND id > ?2))
ORDER BY created_at ASC, id ASC
LIMIT ?3`
		args = []any{createdAt, after.ID, limit}
	}

	var events []task.OutboxEvent
	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(
			conn,
			query,
			&sqlitex.ExecOptions{
				Args: args,
				ResultFunc: func(stmt *sqlite.Stmt) error {
					event, err := scanOutboxEvent(stmt)
					if err != nil {
						return err
					}
					events = append(events, event)
					return nil
				},
			},
		)
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *outboxRepo) MarkSent(ctx context.Context, id string) error {
	return r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(
			conn,
			`UPDATE task_outbox SET sent_at = ? WHERE id = ? AND sent_at IS NULL`,
			&sqlitex.ExecOptions{Args: []any{time.Now().UTC().Format(outboxTimeLayout), id}},
		)
	})
}

func (r *outboxRepo) LatestTraceContext(ctx context.Context, aggregateID uint64, eventType string) (map[string]string, error) {
	var carrier map[string]string
	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(
			conn,
			`SELECT trace_context FROM task_outbox WHERE aggregate_id = ? AND event_type = ?
ORDER BY created_at DESC, id DESC LIMIT 1`,
			&sqlitex.ExecOptions{
				Args: []any{aggregateID, eventType},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					if stmt.ColumnType(0) != sqlite.SQLITE_NULL {
						carrier = task.UnmarshalTraceContext(stmt.ColumnText(0))
					}
					return nil
				},
			},
		)
	})
	return carrier, err
}

func (r *outboxRepo) OutboxBacklog(ctx context.Context) (int64, time.Time, error) {
	var count int64
	var oldest time.Time
	err := r.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(
			conn,
			`SELECT COUNT(*), MIN(created_at) FROM task_outbox WHERE sent_at IS NULL`,
			&sqlitex.ExecOptions{ResultFunc: func(stmt *sqlite.Stmt) error {
				count = stmt.ColumnInt64(0)
				if stmt.ColumnType(1) == sqlite.SQLITE_NULL {
					return nil
				}
				t, perr := time.Parse(outboxTimeLayout, stmt.ColumnText(1))
				if perr != nil {
					return perr
				}
				oldest = t
				return nil
			}},
		)
	})
	return count, oldest, err
}

// scanOutboxEvent maps one FetchUnsent row. Column order: id, aggregate_id,
// destination, event_type, payload, created_at, trace_context.
func scanOutboxEvent(stmt *sqlite.Stmt) (task.OutboxEvent, error) {
	payload := make([]byte, stmt.ColumnLen(4))
	stmt.ColumnBytes(4, payload)

	createdAt, err := time.Parse(outboxTimeLayout, stmt.ColumnText(5))
	if err != nil {
		return task.OutboxEvent{}, fmt.Errorf("parse outbox created_at: %w", err)
	}

	var traceContext map[string]string
	if stmt.ColumnType(6) != sqlite.SQLITE_NULL {
		traceContext = task.UnmarshalTraceContext(stmt.ColumnText(6))
	}

	return task.OutboxEvent{
		ID:           stmt.ColumnText(0),
		AggregateID:  uint64(stmt.ColumnInt64(1)),
		Destination:  stmt.ColumnText(2),
		EventType:    stmt.ColumnText(3),
		Payload:      payload,
		CreatedAt:    createdAt,
		TraceContext: traceContext,
	}, nil
}

// traceContextArg binds an empty carrier as NULL and any other as JSON text.
func traceContextArg(tc map[string]string) any {
	if s, ok := task.MarshalTraceContext(tc); ok {
		return s
	}
	return nil
}
