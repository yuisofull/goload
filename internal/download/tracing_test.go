package download

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
)

type recordingTracing struct {
	mu    sync.Mutex
	spans []string
	ended map[string]error
}

func (r *recordingTracing) StartConsume(ctx context.Context, _ map[string]string, _ uint64) (context.Context, func(error)) {
	return ctx, func(error) {}
}

func (r *recordingTracing) StartSpan(ctx context.Context, name string) (context.Context, func(error)) {
	r.mu.Lock()
	r.spans = append(r.spans, name)
	r.mu.Unlock()
	return ctx, func(err error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.ended == nil {
			r.ended = map[string]error{}
		}
		r.ended[name] = err
	}
}

func TestRunAttempt_TracesTheTransferAndTheFencedCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &recordingTracing{}
		attempts := &fakeAttempts{generation: 3}
		svc := NewService(&keyedStorage{objects: map[string]bool{}}, &busSpy{},
			WithAttemptCoordinator(attempts, "worker-1"), WithTracing(tr))
		svc.RegisterDownloader("HTTP", &fakeDownloader{})

		if err := svc.RunAttempt(t.Context(), attemptRequest()); err != nil {
			t.Fatalf("RunAttempt() error = %v", err)
		}

		if want := []string{"transfer", "complete"}; !reflect.DeepEqual(tr.spans, want) {
			t.Fatalf("spans = %v, want %v", tr.spans, want)
		}
		for _, name := range []string{"transfer", "complete"} {
			if err, ok := tr.ended[name]; !ok || err != nil {
				t.Fatalf("span %q ended=%v err=%v, want ended with nil", name, ok, err)
			}
		}
	})
}
