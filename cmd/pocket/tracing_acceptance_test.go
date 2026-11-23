package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/yuisofull/goload/internal/observability"
	"github.com/yuisofull/goload/internal/task"
)

func recordingTracer(t *testing.T) (*observability.Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return observability.NewTracerFromProvider(tp), rec
}

func spanNamed(rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func waitForSpans(t *testing.T, rec *tracetest.SpanRecorder, names ...string) map[string]sdktrace.ReadOnlySpan {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, n := range names {
			if spanNamed(rec, n) == nil {
				return false
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond, "spans %v never all ended", names)
	out := map[string]sdktrace.ReadOnlySpan{}
	for _, n := range names {
		out[n] = spanNamed(rec, n)
	}
	return out
}

func requireParent(t *testing.T, child, parent sdktrace.ReadOnlySpan) {
	t.Helper()
	require.Equal(t, parent.SpanContext().TraceID(), child.SpanContext().TraceID(), "%q left the trace", child.Name())
	require.Equal(t, parent.SpanContext().SpanID(), child.Parent().SpanID(), "%q has the wrong parent", child.Name())
}

// spanWithID waits for the span to end: the consume span ends only after the
// attempt returns, just after the task is COMPLETED.
func spanWithID(t *testing.T, rec *tracetest.SpanRecorder, id trace.SpanID) sdktrace.ReadOnlySpan {
	t.Helper()
	var found sdktrace.ReadOnlySpan
	require.Eventually(t, func() bool {
		for _, s := range rec.Ended() {
			if s.SpanContext().SpanID() == id {
				found = s
				return true
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond, "span %s never ended", id)
	return found
}

// workingChain returns the publish and process spans above the transfer that
// did the download; the reconciler may add duplicate dispatches beside them.
func workingChain(t *testing.T, rec *tracetest.SpanRecorder, transfer sdktrace.ReadOnlySpan) (publish, process sdktrace.ReadOnlySpan) {
	t.Helper()
	process = spanWithID(t, rec, transfer.Parent().SpanID())
	require.Equal(t, "process task.created", process.Name())
	publish = spanWithID(t, rec, process.Parent().SpanID())
	require.Equal(t, "publish task.created", publish.Name())
	return publish, process
}

// One download is one trace: the create request, the relay publish, the
// worker's consume, its transfer, its fenced completion and the completion
// event. Duplicate dispatches from the reconciler join the same trace.
func TestPocketTracing_OneDownloadIsOneTrace(t *testing.T) {
	tr, rec := recordingTracer(t)
	p := startPipeline(t, &scriptedDownloader{body: "traced"}, pipelineOpts{tracer: tr})

	ctx, endRequest := tr.StartSpan(context.Background(), "POST /api/v1/tasks/create")
	id := p.createIn(t, ctx)
	endRequest(nil)
	p.waitFor(t, id, task.StatusCompleted)

	s := waitForSpans(t, rec, "POST /api/v1/tasks/create", "transfer", "complete", "publish task.completed")
	request := s["POST /api/v1/tasks/create"]
	publish, process := workingChain(t, rec, s["transfer"])
	requireParent(t, publish, request)
	requireParent(t, s["complete"], process)
	requireParent(t, s["publish task.completed"], s["complete"])
	for _, span := range rec.Ended() {
		if span.Name() == "publish task.created" || span.Name() == "process task.created" {
			require.Equal(t, request.SpanContext().TraceID(), span.SpanContext().TraceID(), "a dispatch left the download's trace")
		}
	}
}

// A row written before tracing has no carrier: it still dispatches, as a new
// trace that the worker continues.
func TestPocketTracing_RowWithoutTraceContextStillDispatches(t *testing.T) {
	tr, rec := recordingTracer(t)
	p := startPipeline(t, &scriptedDownloader{body: "untraced row"}, pipelineOpts{tracer: tr})

	id := p.create(t)
	p.waitFor(t, id, task.StatusCompleted)

	s := waitForSpans(t, rec, "transfer")
	publish, _ := workingChain(t, rec, s["transfer"])
	require.False(t, publish.Parent().IsValid(), "a carrier-less row starts a new trace")
}

// A dead trace backend must not affect downloads.
func TestPocketTracing_UnreachableBackendLeavesDownloadsRunning(t *testing.T) {
	tr, err := observability.NewTracer(context.Background(), "pocket", observability.TraceConfig{Endpoint: "http://127.0.0.1:1", SampleRatio: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Shutdown(context.Background()) })
	p := startPipeline(t, &scriptedDownloader{body: "no jaeger"}, pipelineOpts{tracer: tr})

	for range 3 {
		ctx, end := tr.StartSpan(context.Background(), "POST /api/v1/tasks/create")
		id := p.createIn(t, ctx)
		end(nil)
		p.waitFor(t, id, task.StatusCompleted)
	}
}
