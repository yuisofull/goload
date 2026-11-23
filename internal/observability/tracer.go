package observability

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/yuisofull/goload/internal/events"
)

const (
	traceExportTimeout   = 3 * time.Second
	traceShutdownTimeout = 5 * time.Second
)

const dispatchTopic = string(events.EventTaskCreated)

// TraceConfig selects where spans go and how many traces are kept.
type TraceConfig struct {
	// Endpoint is the URL of an OTLP/gRPC receiver (Jaeger:
	// http://jaeger:4317; https for TLS). Empty keeps spans in process: the
	// trace context still propagates and nothing is exported.
	Endpoint string
	// SampleRatio is the share of new traces kept, in [0, 1]. A child always
	// follows its parent's decision.
	SampleRatio float64
}

// Tracer is goload's OTel tracing. It implements the task and download tracing
// ports and is, with the meter, the only code importing the OTel SDK. It never
// sets the global tracer provider: a dependency (the torrent client) traces
// through the global, and those spans must not be exported.
type Tracer struct {
	tp     *sdktrace.TracerProvider
	tracer trace.Tracer
	prop   propagation.TextMapPropagator
}

// NewTracer builds the tracer for serviceName. An unreachable backend never
// fails startup and never slows a caller: the exporter connects lazily, the
// batcher drops spans instead of blocking End, and retries are off.
func NewTracer(ctx context.Context, serviceName string, cfg TraceConfig) (*Tracer, error) {
	if cfg.SampleRatio < 0 || cfg.SampleRatio > 1 {
		return nil, errors.New("trace sample ratio must be within [0, 1]")
	}
	if cfg.Endpoint != "" {
		if u, err := url.Parse(cfg.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("trace endpoint must be an http or https URL")
		}
	}
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(newResource(serviceName)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	}
	if cfg.Endpoint != "" {
		exporter, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpointURL(cfg.Endpoint),
			otlptracegrpc.WithTimeout(traceExportTimeout),
			otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}),
		)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdktrace.WithBatcher(exporter,
			sdktrace.WithExportTimeout(traceExportTimeout+time.Second),
		))
	}
	return NewTracerFromProvider(sdktrace.NewTracerProvider(opts...)), nil
}

// NewTracerFromProvider wraps an existing provider; tests pass one with a span
// recorder.
func NewTracerFromProvider(tp *sdktrace.TracerProvider) *Tracer {
	return &Tracer{
		tp:     tp,
		tracer: tp.Tracer("github.com/yuisofull/goload"),
		prop:   propagation.TraceContext{},
	}
}

// TracerProvider and Propagator are handed explicitly to the otelgrpc/otelhttp
// instrumentation (the global ones are left untouched).
func (t *Tracer) TracerProvider() trace.TracerProvider      { return t.tp }
func (t *Tracer) Propagator() propagation.TextMapPropagator { return t.prop }

// Shutdown flushes pending spans, bounded so a hung backend cannot hold the
// process past its stop grace period.
func (t *Tracer) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, traceShutdownTimeout)
	defer cancel()
	return t.tp.Shutdown(ctx)
}

// Inject returns ctx's trace context as a carrier to store with an outbox row,
// or nil when ctx carries no span.
func (t *Tracer) Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	t.prop.Inject(ctx, carrier)
	if len(carrier) == 0 {
		return nil
	}
	return carrier
}

// StartPublish starts the producer span for an outbox row and returns the
// carrier the message must carry.
func (t *Tracer) StartPublish(ctx context.Context, carrier map[string]string, topic, messageID string) (map[string]string, func(error)) {
	ctx, opts := t.continueFrom(ctx, carrier)
	ctx, span := t.tracer.Start(ctx, "publish "+topic, append(opts,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingDestinationName(topic),
			semconv.MessagingOperationName("publish"),
			semconv.MessagingOperationTypeSend,
			semconv.MessagingMessageID(messageID),
		))...)
	out := propagation.MapCarrier{}
	t.prop.Inject(ctx, out)
	return out, endSpan(span)
}

// StartConsume starts the consumer span for a dispatch message, as a child of
// its producer span so the download keeps one trace ID end to end.
func (t *Tracer) StartConsume(ctx context.Context, carrier map[string]string, taskID uint64) (context.Context, func(error)) {
	ctx, opts := t.continueFrom(ctx, carrier)
	ctx, span := t.tracer.Start(ctx, "process "+dispatchTopic, append(opts,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingDestinationName(dispatchTopic),
			semconv.MessagingOperationName("process"),
			semconv.MessagingOperationTypeProcess,
			attribute.String("goload.task.id", strconv.FormatUint(taskID, 10)),
		))...)
	return ctx, endSpan(span)
}

// StartSpan starts a child span of ctx.
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, func(error)) {
	ctx, span := t.tracer.Start(ctx, name)
	return ctx, endSpan(span)
}

// continueFrom decorates ctx with the remote span in carrier. Extract only
// wraps ctx, so its cancellation, deadline and values are kept. A carrier with
// no valid span starts a new trace rather than inheriting a span ctx already
// holds (the relay reuses one ctx for every row).
func (t *Tracer) continueFrom(ctx context.Context, carrier map[string]string) (context.Context, []trace.SpanStartOption) {
	remote := trace.SpanContextFromContext(t.prop.Extract(context.Background(), propagation.MapCarrier(carrier)))
	if !remote.IsValid() {
		return ctx, []trace.SpanStartOption{trace.WithNewRoot()}
	}
	return t.prop.Extract(ctx, propagation.MapCarrier(carrier)), nil
}

func endSpan(span trace.Span) func(error) {
	return func(err error) {
		if err != nil {
			msg := redactQueries(err.Error())
			span.AddEvent("exception", trace.WithAttributes(
				semconv.ExceptionType(fmt.Sprintf("%T", err)),
				semconv.ExceptionMessage(msg),
			))
			span.SetStatus(codes.Error, msg)
		}
		span.End()
	}
}

var urlQuery = regexp.MustCompile(`(https?://[^\s?#"']*)\?[^\s#"']*`)

// redactQueries drops URL query strings, which may hold presigned credentials.
func redactQueries(s string) string {
	return urlQuery.ReplaceAllString(s, "${1}?REDACTED")
}
