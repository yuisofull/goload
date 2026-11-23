package downloadtransport

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/pkg/message"
)

type consumeSpanKey struct{}

// fakeConsumeTracing decorates the ctx it is given, as the real
// implementation must: the returned ctx derives from the consumer's ctx.
type fakeConsumeTracing struct {
	mu      sync.Mutex
	carrier map[string]string
	taskID  uint64
	ended   []error
}

func (f *fakeConsumeTracing) StartConsume(ctx context.Context, carrier map[string]string, taskID uint64) (context.Context, func(error)) {
	f.mu.Lock()
	f.carrier, f.taskID = carrier, taskID
	f.mu.Unlock()
	return context.WithValue(ctx, consumeSpanKey{}, "consume"), func(err error) {
		f.mu.Lock()
		f.ended = append(f.ended, err)
		f.mu.Unlock()
	}
}

func (f *fakeConsumeTracing) StartSpan(ctx context.Context, _ string) (context.Context, func(error)) {
	return ctx, func(error) {}
}

func (f *fakeConsumeTracing) endErrors() []error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]error(nil), f.ended...)
}

func TestProcessTaskCreatedEvents_ContinuesTheTraceFromTheMessageHeaders(t *testing.T) {
	t.Parallel()

	tr := &fakeConsumeTracing{}
	gotSpan := make(chan any, 1)
	ec := NewEventConsumer(&fakeDownloadService{}, nil, log.NewNopLogger(),
		WithAttemptRunner(&fakeAttemptRunner{runFn: func(ctx context.Context, _ download.TaskRequest) error {
			gotSpan <- ctx.Value(consumeSpanKey{})
			return nil
		}}),
		WithTracing(tr),
	)

	msg := createdMessage(t, 42)
	msg.Metadata["traceparent"] = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	ch := make(chan *message.Message, 1)
	ch <- msg
	close(ch)
	ec.processTaskCreatedEvents(t.Context(), ch)

	assert.Equal(t, "consume", <-gotSpan, "the attempt runs under the consume span")
	assert.Equal(t, "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", tr.carrier["traceparent"])
	assert.Equal(t, uint64(42), tr.taskID)
	assert.Equal(t, []error{nil}, tr.endErrors())
}

// Extracting the trace must decorate the consumer's ctx, never replace it:
// cancelling the consumer still stops a running attempt.
func TestProcessTaskCreatedEvents_CancellingTheConsumerStillCancelsTheAttempt(t *testing.T) {
	t.Parallel()

	tr := &fakeConsumeTracing{}
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ec := NewEventConsumer(&fakeDownloadService{}, nil, log.NewNopLogger(),
		WithAttemptRunner(&fakeAttemptRunner{runFn: func(ctx context.Context, _ download.TaskRequest) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}}),
		WithTracing(tr),
	)

	ch := make(chan *message.Message, 1)
	ch <- createdMessage(t, 42)
	close(ch)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ec.processTaskCreatedEvents(ctx, ch)
	}()

	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the attempt did not observe the consumer's cancellation")
	}
	ended := tr.endErrors()
	require.Len(t, ended, 1)
	assert.ErrorIs(t, ended[0], context.Canceled)
}
