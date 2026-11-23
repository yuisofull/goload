package task

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

const (
	// DefaultOutboxPollInterval is how often the relay polls for unsent events.
	DefaultOutboxPollInterval = 500 * time.Millisecond
	// DefaultOutboxBatchSize is the maximum number of events handled per poll.
	DefaultOutboxBatchSize = 100

	// outboxMarkSentTimeout bounds MarkSent, which deliberately ignores relay
	// shutdown so an already-published event is not left unsent.
	outboxMarkSentTimeout = 5 * time.Second
)

// OutboxRelay polls the outbox, sweeps all unsent events each pass and publishes them on the existing
// transport, then marks them sent. Delivery is at-least-once: when the process
// dies or MarkSent fails after a successful publish, the same event is
// published again with the same message UUID. Consumers do not dedupe yet;
// dedup by event id is later work.
type OutboxRelay struct {
	repo      OutboxRepository
	pub       *Publisher
	interval  time.Duration
	batchSize int
	logger    log.Logger
	metrics   Metrics
	tracing   Tracing
}

// OutboxRelayOption configures an OutboxRelay.
type OutboxRelayOption func(*OutboxRelay)

// WithOutboxPollInterval overrides DefaultOutboxPollInterval. Values that are
// zero or negative are ignored.
func WithOutboxPollInterval(d time.Duration) OutboxRelayOption {
	return func(r *OutboxRelay) {
		if d > 0 {
			r.interval = d
		}
	}
}

// WithOutboxBatchSize overrides DefaultOutboxBatchSize. Values that are zero
// or negative are ignored.
func WithOutboxBatchSize(n int) OutboxRelayOption {
	return func(r *OutboxRelay) {
		if n > 0 {
			r.batchSize = n
		}
	}
}

// WithOutboxRelayLogger sets the relay logger.
func WithOutboxRelayLogger(l log.Logger) OutboxRelayOption {
	return func(r *OutboxRelay) { r.logger = l }
}

// WithOutboxRelayMetrics injects a metrics recorder. Nil keeps the no-op default.
func WithOutboxRelayMetrics(m Metrics) OutboxRelayOption {
	return func(r *OutboxRelay) {
		if m != nil {
			r.metrics = m
		}
	}
}

// NewOutboxRelay creates a relay that reads from repo and publishes through pub.
func NewOutboxRelay(repo OutboxRepository, pub *Publisher, opts ...OutboxRelayOption) *OutboxRelay {
	r := &OutboxRelay{
		repo:      repo,
		pub:       pub,
		interval:  DefaultOutboxPollInterval,
		batchSize: DefaultOutboxBatchSize,
		logger:    log.NewNopLogger(),
		metrics:   nopMetrics{},
		tracing:   nopTracing{},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Run polls until ctx is cancelled and then returns ctx.Err(). The first pass
// runs immediately, so events left unsent by a previous process are published
// on startup.
func (r *OutboxRelay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		sent, err := r.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			_ = level.Warn(r.logger).
				Log("msg", "outbox relay pass incomplete, will retry", "sent", sent, "err", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOnce performs a single full sweep: it pages through every unsent event
// (batchSize rows per fetch, using a cursor), publishes each one and marks it
// sent. It returns how many events were published and marked, and the first
// error encountered. A failed event is skipped: the cursor advances past it,
// so it cannot starve the events behind it this pass; it stays unsent and is
// retried on the next pass, which starts again from the oldest row.
func (r *OutboxRelay) RunOnce(ctx context.Context) (int, error) {
	var (
		sent     int
		firstErr error
		cursor   OutboxCursor
	)
	for {
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		batch, err := r.repo.FetchUnsent(ctx, cursor, r.batchSize)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("fetch unsent outbox events: %w", err)
			}
			return sent, firstErr
		}
		if len(batch) == 0 {
			return sent, firstErr
		}
		for _, event := range batch {
			if err := ctx.Err(); err != nil {
				return sent, err
			}
			if err := r.relayOne(ctx, event); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				sent++
			}
			// Advance past this event (success or failure) so a stuck row
			// does not block the rows behind it this pass; failures are
			// retried on the next pass (the cursor resets each pass).
			cursor = OutboxCursor{CreatedAt: event.CreatedAt, ID: event.ID}
		}
		if len(batch) < r.batchSize {
			return sent, firstErr
		}
	}
}

func (r *OutboxRelay) relayOne(ctx context.Context, event OutboxEvent) error {
	// The publish span continues the trace stored with the row; the message
	// carries the span's own carrier so the consumer continues from it.
	topic, _ := outboxTopic(event.EventType)
	outgoing, endSpan := r.tracing.StartPublish(ctx, event.TraceContext, topic, event.ID)
	event.TraceContext = outgoing
	err := r.pub.PublishOutboxEvent(ctx, event)
	endSpan(err)
	if err != nil {
		r.metrics.RecordRelayPublish(false)
		return fmt.Errorf("publish outbox event %s: %w", event.ID, err)
	}
	r.metrics.RecordRelayPublish(true)

	// The event is on the wire. Mark it even if ctx was cancelled meanwhile,
	// so a clean shutdown does not become a duplicate publish on the next start.
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outboxMarkSentTimeout)
	defer cancel()
	if err := r.repo.MarkSent(markCtx, event.ID); err != nil {
		// Published but not marked: it is published again on the next pass.
		return fmt.Errorf("mark outbox event %s sent: %w", event.ID, err)
	}
	return nil
}
