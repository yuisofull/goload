package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	task "github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/internal/task/mysql/sqlc"
)

type outboxRepo struct {
	db      *sql.DB
	queries *sqlc.Queries
}

// NewOutboxRepo returns the MySQL implementation of task.OutboxRepository. It
// uses the sqlc-generated queries and needs parseTime=true in the DSN, which
// cmd/task already sets.
func NewOutboxRepo(db *sql.DB) task.OutboxRepository {
	return &outboxRepo{db: db, queries: sqlc.New(db)}
}

// q binds the sqlc queries to the transaction DoInTx stored in ctx, or to the
// plain *sql.DB when there is none.
func (r *outboxRepo) q(ctx context.Context) *sqlc.Queries {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return r.queries.WithTx(tx)
	}
	return r.queries
}

func (r *outboxRepo) InsertUnsent(ctx context.Context, event task.OutboxEvent) error {
	_, err := r.q(ctx).InsertOutbox(ctx, sqlc.InsertOutboxParams{
		ID:          event.ID,
		AggregateID: event.AggregateID,
		Destination: event.Destination,
		EventType:   event.EventType,
		Payload:     event.Payload,
		// DATETIME(6) keeps microseconds; truncate so MySQL does not round up.
		CreatedAt:    event.CreatedAt.UTC().Truncate(time.Microsecond),
		TraceContext: traceContextColumn(event.TraceContext),
	})
	return err
}

// traceContextColumn stores an empty carrier as NULL and any other as JSON.
func traceContextColumn(tc map[string]string) sql.NullString {
	s, ok := task.MarshalTraceContext(tc)
	return sql.NullString{String: s, Valid: ok}
}

func (r *outboxRepo) FetchUnsent(ctx context.Context, after task.OutboxCursor, limit int) ([]task.OutboxEvent, error) {
	// MySQL rejects a negative LIMIT.
	if limit <= 0 {
		return nil, nil
	}

	var (
		rows []sqlc.TaskOutbox
		err  error
	)
	if after.CreatedAt.IsZero() {
		rows, err = r.q(ctx).FetchUnsentOutbox(ctx, int32(limit))
	} else {
		rows, err = r.q(ctx).FetchUnsentOutboxAfter(ctx, sqlc.FetchUnsentOutboxAfterParams{
			CreatedAt:   after.CreatedAt,
			CreatedAt_2: after.CreatedAt,
			ID:          after.ID,
			Limit:       int32(limit),
		})
	}
	if err != nil {
		return nil, err
	}

	var events []task.OutboxEvent
	for _, row := range rows {
		events = append(events, outboxEventFromRow(row))
	}
	return events, nil
}

func (r *outboxRepo) MarkSent(ctx context.Context, id string) error {
	return r.q(ctx).MarkOutboxSent(ctx, sqlc.MarkOutboxSentParams{
		SentAt: sql.NullTime{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true},
		ID:     id,
	})
}

func (r *outboxRepo) LatestTraceContext(ctx context.Context, aggregateID uint64, eventType string) (map[string]string, error) {
	tc, err := r.q(ctx).LatestOutboxTraceContext(ctx, sqlc.LatestOutboxTraceContextParams{
		AggregateID: aggregateID,
		EventType:   eventType,
	})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !tc.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return task.UnmarshalTraceContext(tc.String), nil
}

func (r *outboxRepo) OutboxBacklog(ctx context.Context) (int64, time.Time, error) {
	row, err := r.q(ctx).OutboxBacklog(ctx)
	if err != nil {
		return 0, time.Time{}, err
	}
	// MIN(created_at) is interface{}; with parseTime it is time.Time, or nil
	// when the backlog is empty.
	oldest, _ := row.Oldest.(time.Time)
	return row.Unsent, oldest, nil
}

// outboxEventFromRow maps a generated task_outbox row to the domain event.
func outboxEventFromRow(row sqlc.TaskOutbox) task.OutboxEvent {
	event := task.OutboxEvent{
		ID:          row.ID,
		AggregateID: row.AggregateID,
		Destination: row.Destination,
		EventType:   row.EventType,
		Payload:     row.Payload,
		CreatedAt:   row.CreatedAt,
	}
	if row.SentAt.Valid {
		sentAt := row.SentAt.Time
		event.SentAt = &sentAt
	}
	if row.TraceContext.Valid {
		event.TraceContext = task.UnmarshalTraceContext(row.TraceContext.String)
	}
	return event
}
