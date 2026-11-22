package task

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/pkg/message/inmem"
)

var errMarkFailed = errors.New("mark sent failed")

// memOutbox is an in-memory OutboxRepository for relay and service tests.
type memOutbox struct {
	mu             sync.Mutex
	rows           []OutboxEvent
	insertErr      error
	fetchErr       error
	markAttempts   map[string]int // MarkSent calls per event id
	failFirstMarks map[string]int // fail this many first MarkSent calls per event id
	markCtxErrs    []error        // ctx.Err() seen by each MarkSent call
}

func (o *memOutbox) InsertUnsent(_ context.Context, event OutboxEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.insertErr != nil {
		return o.insertErr
	}
	o.rows = append(o.rows, event)
	return nil
}

func (o *memOutbox) FetchUnsent(_ context.Context, after OutboxCursor, limit int) ([]OutboxEvent, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fetchErr != nil {
		return nil, o.fetchErr
	}
	if limit <= 0 {
		return nil, nil
	}
	var out []OutboxEvent
	for _, row := range o.rows {
		if row.SentAt != nil {
			continue
		}
		if !after.CreatedAt.IsZero() {
			if row.CreatedAt.Before(after.CreatedAt) ||
				(row.CreatedAt.Equal(after.CreatedAt) && row.ID <= after.ID) {
				continue
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (o *memOutbox) MarkSent(ctx context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.markCtxErrs = append(o.markCtxErrs, ctx.Err())
	if o.markAttempts == nil {
		o.markAttempts = map[string]int{}
	}
	o.markAttempts[id]++
	if o.markAttempts[id] <= o.failFirstMarks[id] {
		return errMarkFailed
	}
	for i := range o.rows {
		if o.rows[i].ID == id && o.rows[i].SentAt == nil {
			now := time.Now().UTC()
			o.rows[i].SentAt = &now
		}
	}
	return nil
}

func (o *memOutbox) OutboxBacklog(_ context.Context) (int64, time.Time, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var count int64
	var oldest time.Time
	for _, row := range o.rows {
		if row.SentAt != nil {
			continue
		}
		count++
		if oldest.IsZero() || row.CreatedAt.Before(oldest) {
			oldest = row.CreatedAt
		}
	}
	return count, oldest, nil
}

func (o *memOutbox) snapshot() []OutboxEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]OutboxEvent(nil), o.rows...)
}

func (o *memOutbox) unsentIDs() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	ids := []string{}
	for _, row := range o.rows {
		if row.SentAt == nil {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

func seededOutbox(t *testing.T, events ...OutboxEvent) *memOutbox {
	t.Helper()
	outbox := &memOutbox{}
	for _, event := range events {
		require.NoError(t, outbox.InsertUnsent(context.Background(), event))
	}
	return outbox
}

// waitRelayStopped fails the test when Run does not return shortly after cancel.
func waitRelayStopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx was cancelled")
	}
}

func TestNewOutboxRelay_DefaultsAndInvalidOptions(t *testing.T) {
	require.Equal(t, 500*time.Millisecond, DefaultOutboxPollInterval)
	require.Equal(t, 100, DefaultOutboxBatchSize)

	relay := NewOutboxRelay(
		&memOutbox{},
		NewEventPublisher(&recordingPublisher{}),
		WithOutboxPollInterval(0),
		WithOutboxBatchSize(-1),
	)
	require.Equal(t, DefaultOutboxPollInterval, relay.interval, "a non-positive interval must be ignored")
	require.Equal(t, DefaultOutboxBatchSize, relay.batchSize, "a non-positive batch size must be ignored")
}

func TestOutboxRelay_RunOnce_PublishesThenMarksSent(t *testing.T) {
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1), outboxEventFixture("evt-2", 2))
	transport := &recordingPublisher{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport))

	sent, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, sent)

	published := transport.snapshot()
	require.Len(t, published, 2)
	require.Equal(t, "task.created", published[0].topic)
	require.Equal(t, "task.created", published[1].topic)
	require.Equal(t, []string{"evt-1", "evt-2"}, transport.uuids())
	require.Equal(t, `{"task_id":1}`, string(published[0].msg.Payload))
	require.Empty(t, outbox.unsentIDs())

	// Nothing left: a second pass publishes nothing.
	sent, err = relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, sent)
	require.Len(t, transport.snapshot(), 2)
}

func TestOutboxRelay_RunOnce_PublishFailureLeavesRowUnsent(t *testing.T) {
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1), outboxEventFixture("evt-2", 2))
	transport := &recordingPublisher{failFirst: map[string]int{"evt-1": 1}}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport))

	sent, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, errPublishFailed)
	require.Equal(t, 1, sent, "the event behind the failed one must still go out")
	require.Equal(t, []string{"evt-2"}, transport.uuids())
	require.Equal(t, []string{"evt-1"}, outbox.unsentIDs())

	// The failed event is retried on the next pass.
	sent, err = relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	require.Equal(t, []string{"evt-2", "evt-1"}, transport.uuids())
	require.Empty(t, outbox.unsentIDs())
}

func TestOutboxRelay_RunOnce_MarkSentFailureRepublishesSameEvent(t *testing.T) {
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1))
	outbox.failFirstMarks = map[string]int{"evt-1": 1}
	transport := &recordingPublisher{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport))

	sent, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, errMarkFailed)
	require.Zero(t, sent)
	require.Equal(t, []string{"evt-1"}, outbox.unsentIDs(), "published but not marked: must stay unsent")

	sent, err = relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	require.Empty(t, outbox.unsentIDs())

	// At-least-once: the duplicate carries the same UUID and payload.
	published := transport.snapshot()
	require.Len(t, published, 2)
	require.Equal(t, []string{"evt-1", "evt-1"}, transport.uuids())
	require.Equal(t, string(published[0].msg.Payload), string(published[1].msg.Payload))
}

func TestOutboxRelay_RunOnce_StopsOnCancelButMarksPublishedEvent(t *testing.T) {
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1), outboxEventFixture("evt-2", 2))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Shutdown arrives while the first event is being published.
	transport := &recordingPublisher{onPublish: cancel}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport))

	sent, err := relay.RunOnce(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, sent)
	require.Equal(t, []string{"evt-1"}, transport.uuids(), "no event may be published after cancel")
	require.Equal(t, []string{"evt-2"}, outbox.unsentIDs())
	require.Equal(t, []error{nil}, outbox.markCtxErrs, "MarkSent must run on a context that is not cancelled")
}

func TestOutboxRelay_RunOnce_FetchError(t *testing.T) {
	fetchErr := errors.New("db down")
	outbox := &memOutbox{fetchErr: fetchErr}
	transport := &recordingPublisher{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport))

	sent, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, fetchErr)
	require.Zero(t, sent)
	require.Empty(t, transport.snapshot())
}

func TestOutboxRelay_RunOnce_SweepsAllRowsInBatches(t *testing.T) {
	outbox := seededOutbox(t,
		outboxEventFixture("evt-1", 1),
		outboxEventFixture("evt-2", 2),
		outboxEventFixture("evt-3", 3),
	)
	transport := &recordingPublisher{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxBatchSize(2))

	// Batch size 2 pages the fetch; one pass still covers all three rows.
	sent, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, sent)
	require.Equal(t, []string{"evt-1", "evt-2", "evt-3"}, transport.uuids())

	sent, err = relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, sent)
}

func TestOutboxRelay_Run_FirstPassIsImmediate(t *testing.T) {
	// A row left unsent by a previous process must not wait a full interval.
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1))
	transport := &recordingPublisher{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	require.Eventually(t, func() bool {
		return len(transport.snapshot()) == 1
	}, 2*time.Second, 5*time.Millisecond)

	cancel()
	waitRelayStopped(t, done)
}

func TestOutboxRelay_Run_PollsUntilCancelled(t *testing.T) {
	outbox := &memOutbox{}
	transport := &recordingPublisher{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxPollInterval(5*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	require.NoError(t, outbox.InsertUnsent(ctx, outboxEventFixture("evt-1", 1)))
	require.Eventually(t, func() bool {
		return len(transport.snapshot()) == 1
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, outbox.InsertUnsent(ctx, outboxEventFixture("evt-2", 2)))
	require.Eventually(t, func() bool {
		return len(transport.snapshot()) == 2 && len(outbox.unsentIDs()) == 0
	}, 2*time.Second, 5*time.Millisecond)

	cancel()
	waitRelayStopped(t, done)
	require.Equal(t, []string{"evt-1", "evt-2"}, transport.uuids())
}

func TestOutboxRelay_Inmem_WaitsForSubscriber(t *testing.T) {
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 42))
	msgPub, msgSub := inmem.NewPublisherAndSubscriber(log.NewNopLogger())
	relay := NewOutboxRelay(outbox, NewEventPublisher(msgPub, WithOutboxTransport(NewRequireSubscriberTransport(msgPub))))

	// Pocket startup race: the relay runs before the download consumer has
	// subscribed. The in-memory bus would drop the message, so it must stay
	// unsent instead of being marked sent.
	sent, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, inmem.ErrNoSubscribers)
	require.Zero(t, sent)
	require.Equal(t, []string{"evt-1"}, outbox.unsentIDs())

	ch, err := msgSub.Subscribe(t.Context(), "task.created")
	require.NoError(t, err)

	sent, err = relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	require.Empty(t, outbox.unsentIDs())

	select {
	case msg := <-ch:
		require.NotNil(t, msg)
		require.Equal(t, "evt-1", msg.UUID)
		require.Equal(t, `{"task_id":42}`, string(msg.Payload))
		msg.Ack()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for task.created message")
	}
}

func TestOutboxRelay_RunOnce_StuckRowsDoNotStarveTheTail(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	withTime := func(id string, taskID uint64, offset time.Duration) OutboxEvent {
		event := outboxEventFixture(id, taskID)
		event.CreatedAt = base.Add(offset)
		return event
	}
	outbox := seededOutbox(t,
		withTime("evt-1", 1, 0),
		withTime("evt-2", 2, time.Millisecond),
		withTime("evt-3", 3, 2*time.Millisecond),
	)
	// The two oldest rows always fail; with batch size 2 the old relay
	// re-fetched only those two on every pass and never reached evt-3.
	transport := &recordingPublisher{failFirst: map[string]int{"evt-1": 1 << 30, "evt-2": 1 << 30}}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxBatchSize(2))

	sent, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, errPublishFailed)
	require.Equal(t, 1, sent, "the healthy tail row must be published in a single pass")
	require.Equal(t, []string{"evt-3"}, transport.uuids())
	require.Equal(t, []string{"evt-1", "evt-2"}, outbox.unsentIDs())
}
