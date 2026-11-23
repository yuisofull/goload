package observability

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/task"
)

func newRecordingTracer(t *testing.T) (*Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return NewTracerFromProvider(tp), rec
}

func endedSpan(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("no ended span %q", name)
	return nil
}

func requireChildOf(t *testing.T, child, parent sdktrace.ReadOnlySpan) {
	t.Helper()
	if child.SpanContext().TraceID() != parent.SpanContext().TraceID() {
		t.Fatalf("%q trace %s, want %s", child.Name(), child.SpanContext().TraceID(), parent.SpanContext().TraceID())
	}
	if child.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("%q parent %s, want %s", child.Name(), child.Parent().SpanID(), parent.SpanContext().SpanID())
	}
}

// One trace across the outbox: request -> publish -> consume -> worker span.
func TestTracer_CarriesOneTraceAcrossTheOutboxAndTheBroker(t *testing.T) {
	tr, rec := newRecordingTracer(t)

	reqCtx, req := tr.tracer.Start(context.Background(), "request")
	stored := tr.Inject(reqCtx)
	req.End()
	if stored["traceparent"] == "" {
		t.Fatalf("Inject stored no traceparent: %v", stored)
	}

	headers, endPublish := tr.StartPublish(context.Background(), stored, "task.created", "evt-1")
	endPublish(nil)

	consumeCtx, endConsume := tr.StartConsume(context.Background(), headers, 42)
	_, endTransfer := tr.StartSpan(consumeCtx, "transfer")
	endTransfer(nil)
	endConsume(nil)

	request := endedSpan(t, rec, "request")
	publish := endedSpan(t, rec, "publish task.created")
	process := endedSpan(t, rec, "process task.created")
	transfer := endedSpan(t, rec, "transfer")
	requireChildOf(t, publish, request)
	requireChildOf(t, process, publish)
	requireChildOf(t, transfer, process)
	if publish.SpanKind() != trace.SpanKindProducer || process.SpanKind() != trace.SpanKindConsumer {
		t.Fatalf("kinds publish=%v process=%v, want producer/consumer", publish.SpanKind(), process.SpanKind())
	}
}

func TestTracer_InjectWithoutASpanStoresNothing(t *testing.T) {
	tr, _ := newRecordingTracer(t)
	if got := tr.Inject(context.Background()); got != nil {
		t.Fatalf("Inject without a span = %v, want nil", got)
	}
}

// A row written before tracing (no carrier) starts a new trace and still
// hands the consumer a carrier.
func TestTracer_EmptyCarrierStartsANewTrace(t *testing.T) {
	tr, rec := newRecordingTracer(t)

	headers, end := tr.StartPublish(context.Background(), nil, "task.created", "evt-old")
	end(nil)

	publish := endedSpan(t, rec, "publish task.created")
	if publish.Parent().IsValid() {
		t.Fatalf("an empty carrier must start a new root, got parent %s", publish.Parent().SpanID())
	}
	if headers["traceparent"] == "" {
		t.Fatal("the publish span must still be sent to the consumer")
	}
}

// The relay reuses one loop ctx for every row: a row without a carrier must not
// inherit the previous row's trace.
func TestTracer_PublishDoesNotLeakOneRowsTraceIntoTheNext(t *testing.T) {
	tr, rec := newRecordingTracer(t)
	loopCtx, ambient := tr.tracer.Start(context.Background(), "ambient")
	defer ambient.End()

	reqCtx, req := tr.tracer.Start(context.Background(), "request")
	stored := tr.Inject(reqCtx)
	req.End()

	_, endA := tr.StartPublish(loopCtx, stored, "task.created", "evt-a")
	endA(nil)
	_, endB := tr.StartPublish(loopCtx, nil, "task.created", "evt-b")
	endB(nil)

	var withParent, roots int
	for _, s := range rec.Ended() {
		if s.Name() != "publish task.created" {
			continue
		}
		if s.Parent().IsValid() {
			withParent++
			if s.SpanContext().TraceID() != endedSpan(t, rec, "request").SpanContext().TraceID() {
				t.Fatal("the row with a carrier must continue its own trace")
			}
		} else {
			roots++
		}
	}
	if withParent != 1 || roots != 1 {
		t.Fatalf("publish spans withParent=%d roots=%d, want 1 and 1", withParent, roots)
	}
}

type ctxKey struct{}

// Continuing a trace decorates the consumer's ctx; it never replaces it.
func TestTracer_StartConsumeKeepsTheConsumersCancellationAndValues(t *testing.T) {
	tr, _ := newRecordingTracer(t)
	base, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "kept"))

	headers, end := tr.StartPublish(context.Background(), nil, "task.created", "evt-1")
	end(nil)
	consumeCtx, endConsume := tr.StartConsume(base, headers, 7)
	defer endConsume(nil)

	if consumeCtx.Value(ctxKey{}) != "kept" {
		t.Fatal("the consumer's values were dropped")
	}
	cancel()
	select {
	case <-consumeCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelling the consumer did not cancel the span ctx")
	}
}

func TestTracer_SpanEndsWithTheStepError(t *testing.T) {
	tr, rec := newRecordingTracer(t)
	_, end := tr.StartSpan(context.Background(), "complete")
	end(errors.New("fenced"))

	span := endedSpan(t, rec, "complete")
	if span.Status().Code != codes.Error {
		t.Fatalf("status = %v, want Error", span.Status().Code)
	}
}

// A dead or hung trace backend must never slow the caller.
func TestNewTracer_UnreachableBackendNeverBlocksEndOrShutdown(t *testing.T) {
	blackhole, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blackhole.Close()
	go func() {
		for {
			conn, err := blackhole.Accept()
			if err != nil {
				return
			}
			defer conn.Close() // accept, never answer
		}
	}()

	for name, endpoint := range map[string]string{"closed port": "127.0.0.1:1", "blackhole": blackhole.Addr().String()} {
		t.Run(name, func(t *testing.T) {
			tr, err := NewTracer(context.Background(), "test", TraceConfig{Endpoint: "http://" + endpoint, SampleRatio: 1})
			if err != nil {
				t.Fatalf("NewTracer must not fail on an unreachable backend: %v", err)
			}
			start := time.Now()
			for i := 0; i < 5000; i++ {
				_, end := tr.StartSpan(context.Background(), "work")
				end(nil)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("5000 spans took %v against a dead backend", elapsed)
			}
			start = time.Now()
			_ = tr.Shutdown(context.Background())
			if elapsed := time.Since(start); elapsed > 7*time.Second {
				t.Fatalf("Shutdown took %v; it must be bounded", elapsed)
			}
		})
	}
}

func TestNewTracer_RejectsAnInvalidSampleRatio(t *testing.T) {
	if _, err := NewTracer(context.Background(), "test", TraceConfig{SampleRatio: 1.5}); err == nil {
		t.Fatal("a sample ratio above 1 must be rejected")
	}
}

func TestNewTracer_RejectsAnEndpointThatIsNotAnHTTPURL(t *testing.T) {
	for _, endpoint := range []string{"jaeger:4317", "grpc://jaeger:4317", "http://"} {
		if _, err := NewTracer(context.Background(), "test", TraceConfig{Endpoint: endpoint, SampleRatio: 1}); err == nil {
			t.Fatalf("endpoint %q must be rejected", endpoint)
		}
	}
}

// Source URLs may carry presigned credentials in their query string; span
// errors keep the message but never the query.
func TestTracer_SpanErrorsDropURLQueryStrings(t *testing.T) {
	tr, rec := newRecordingTracer(t)
	_, end := tr.StartSpan(context.Background(), "transfer")
	end(errors.New(`GET "https://host.test/f.bin?X-Amz-Signature=secret&a=b": status 403`))

	span := endedSpan(t, rec, "transfer")
	want := `GET "https://host.test/f.bin?REDACTED": status 403`
	if span.Status().Description != want {
		t.Fatalf("status = %q, want %q", span.Status().Description, want)
	}
	for _, ev := range span.Events() {
		for _, a := range ev.Attributes {
			if strings.Contains(a.Value.Emit(), "secret") {
				t.Fatalf("event %q leaks the query string: %s", ev.Name, a.Value.Emit())
			}
		}
	}
}

// The tracer is what cmd injects into both services' tracing ports.
var (
	_ task.Tracing     = (*Tracer)(nil)
	_ download.Tracing = (*Tracer)(nil)
)
