package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
)

type fakeTracing struct {
	mu       sync.Mutex
	inject   map[string]string   // returned by Inject
	outgoing map[string]string   // returned by StartPublish
	parents  []map[string]string // carriers StartPublish received
	ended    []error
}

func (f *fakeTracing) Inject(context.Context) map[string]string { return f.inject }

func (f *fakeTracing) StartPublish(_ context.Context, carrier map[string]string, _, _ string) (map[string]string, func(error)) {
	f.mu.Lock()
	f.parents = append(f.parents, carrier)
	f.mu.Unlock()
	return f.outgoing, func(err error) {
		f.mu.Lock()
		f.ended = append(f.ended, err)
		f.mu.Unlock()
	}
}

func TestCreateTask_CapturesTraceContextOnTheOutboxRow(t *testing.T) {
	outbox := &memOutbox{}
	tr := &fakeTracing{inject: map[string]string{"traceparent": "00-aaaa-bbbb-01"}}
	svc := NewService(&fakeRepo{}, *NewEventPublisher(&fakeMessagePublisher{}), fakeTxManager{}, outbox,
		newMemAttempts(), WithTracing(tr))

	_, err := svc.CreateTask(context.Background(), &CreateTaskParam{
		OfAccountID: 7,
		FileName:    "file.bin",
		SourceURL:   "https://example.test/file.bin",
	})
	require.NoError(t, err)

	queued := outbox.snapshot()
	require.Len(t, queued, 1)
	require.Equal(t, tr.inject, queued[0].TraceContext)
}

func TestOutboxRelay_PublishSpanCarrierReachesMessageMetadata(t *testing.T) {
	ev := outboxEventFixture("evt-1", 1)
	ev.TraceContext = map[string]string{"traceparent": "00-producer"}
	outbox := seededOutbox(t, ev)
	transport := &recordingPublisher{}
	tr := &fakeTracing{outgoing: map[string]string{"traceparent": "00-publish-span"}}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxRelayTracing(tr))

	sent, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sent)

	require.Equal(t, []map[string]string{{"traceparent": "00-producer"}}, tr.parents,
		"the publish span is parented on the carrier stored with the row")
	published := transport.snapshot()
	require.Len(t, published, 1)
	require.Equal(t, "00-publish-span", published[0].msg.Metadata["traceparent"])
	require.Equal(t, ev.EventType, published[0].msg.Metadata["eventType"],
		"trace headers sit beside, not over, the existing metadata")
	require.Equal(t, []error{nil}, tr.ended)
}

// A row written before tracing has no carrier; it must still dispatch.
func TestOutboxRelay_EventWithoutTraceContextStillPublishes(t *testing.T) {
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1))
	transport := &recordingPublisher{}
	tr := &fakeTracing{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxRelayTracing(tr))

	sent, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	require.Empty(t, outbox.unsentIDs())
	require.Equal(t, []map[string]string{nil}, tr.parents)
	_, has := transport.snapshot()[0].msg.Metadata["traceparent"]
	require.False(t, has)
}

func TestOutboxRelay_PublishSpanEndsWithThePublishError(t *testing.T) {
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1))
	transport := &recordingPublisher{failFirst: map[string]int{"evt-1": 1}}
	tr := &fakeTracing{}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxRelayTracing(tr))

	_, err := relay.RunOnce(context.Background())
	require.Error(t, err)
	require.Len(t, tr.ended, 1)
	require.Error(t, tr.ended[0])
}

// The lifecycle events the worker's completion and terminal failure write join
// the download's trace.
func TestAttemptService_LifecycleRowsCarryTheTrace(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	lease := now.Add(time.Minute)
	carrier := map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}

	for name, settle := range map[string]func(*AttemptService) error{
		"completed": func(s *AttemptService) error {
			return s.CompleteAttempt(context.Background(), 9, "w", 1, events.AttemptResult{StorageKey: "9/1/f", StorageType: storage.TypeLocal})
		},
		"failed": func(s *AttemptService) error {
			return s.FailAttempt(context.Background(), 9, "w", 1, errors.New("boom"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			outbox := &memOutbox{}
			svc := NewAttemptService(
				newMemAttempts(AttemptState{TaskID: 9, Status: StatusStoring, OwnerID: "w", Generation: 1, LeaseExpiresAt: &lease}),
				fakeTxManager{}, outbox,
				WithAttemptClock(func() time.Time { return now }),
				WithAttemptTracing(&fakeTracing{inject: carrier}),
			)
			require.NoError(t, settle(svc))

			rows := outbox.snapshot()
			require.Len(t, rows, 1)
			require.Equal(t, carrier, rows[0].TraceContext)
		})
	}
}
