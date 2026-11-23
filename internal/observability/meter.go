// Package observability builds the OpenTelemetry meter provider and its
// Prometheus /metrics handler. It is the only place that imports otel, so core
// packages stay free of it (CC-2); services reach it through their own metrics
// ports, wired to a Recorder in cmd.
package observability

import (
	"context"
	"net/http"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

// Recorder records goload's domain metrics. Push methods are called from the
// task and download services through their metrics ports; observable gauges are
// registered from cmd (see Register* methods added with their features).
type Recorder struct {
	mp    *sdkmetric.MeterProvider
	meter metric.Meter

	dispatchWait metric.Float64Histogram
	transfer     metric.Float64Histogram
	completed    metric.Int64Counter
	failed       metric.Int64Counter
	retried      metric.Int64Counter
	redelivered  metric.Int64Counter
	redispatched metric.Int64Counter
	relay        metric.Int64Counter
}

// Boundaries reach past the 30-minute task timeout so a slow download is not
// pinned to the top bucket (which would make p50/p95 read the same for a
// 2-minute and a 25-minute transfer). Dispatch wait can run even longer while
// workers are down.
var secondsBuckets = metric.WithExplicitBucketBoundaries(.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600)

// NewRecorder builds a meter provider exporting to a private Prometheus registry
// and returns the Recorder plus the /metrics handler. A missing or failing
// scraper never affects the caller: the reader is pull-only.
func NewRecorder(serviceName string) (*Recorder, http.Handler, error) {
	reg := promclient.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(reg), otelprom.WithoutScopeInfo())
	if err != nil {
		return nil, nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(newResource(serviceName)),
		// otelhttp fills server.address/server.port from the client-controlled
		// Host header; drop them so a public /metrics cannot be grown without
		// bound. The cardinality limit is a backstop for any other instrument.
		sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: "http.server.*"},
			sdkmetric.Stream{AttributeFilter: attribute.NewDenyKeysFilter("server.address", "server.port")},
		)),
		sdkmetric.WithCardinalityLimit(2000),
	)
	m := mp.Meter("goload")

	r := &Recorder{mp: mp, meter: m}
	if err := r.init(m); err != nil {
		return nil, nil, err
	}

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	return r, handler, nil
}

func (r *Recorder) init(m metric.Meter) error {
	var err error
	if r.dispatchWait, err = m.Float64Histogram("goload_dispatch_wait",
		metric.WithUnit("s"),
		metric.WithDescription("Seconds from task creation to first committed claim."),
		secondsBuckets); err != nil {
		return err
	}
	if r.transfer, err = m.Float64Histogram("goload_transfer_duration",
		metric.WithUnit("s"),
		metric.WithDescription("Seconds streaming source bytes, excluding probe and completion."),
		secondsBuckets); err != nil {
		return err
	}
	if r.completed, err = m.Int64Counter("goload_tasks_completed",
		metric.WithUnit("{task}"), metric.WithDescription("Tasks completed.")); err != nil {
		return err
	}
	if r.failed, err = m.Int64Counter("goload_tasks_failed",
		metric.WithUnit("{task}"), metric.WithDescription("Tasks terminally failed.")); err != nil {
		return err
	}
	if r.retried, err = m.Int64Counter("goload_tasks_retried",
		metric.WithUnit("{task}"), metric.WithDescription("Attempts rescheduled for retry.")); err != nil {
		return err
	}
	if r.redelivered, err = m.Int64Counter("goload_redelivered",
		metric.WithUnit("{message}"), metric.WithDescription("Dispatch messages redelivered by the broker.")); err != nil {
		return err
	}
	if r.redispatched, err = m.Int64Counter("goload_reconciler_redispatched",
		metric.WithUnit("{task}"), metric.WithDescription("Tasks redispatched by the reconciler.")); err != nil {
		return err
	}
	if r.relay, err = m.Int64Counter("goload_relay_published",
		metric.WithUnit("{event}"), metric.WithDescription("Outbox events published, by result.")); err != nil {
		return err
	}

	// Zero-init counter series so rate()/increase() see the first real event.
	ctx := context.Background()
	for _, c := range []metric.Int64Counter{r.completed, r.failed, r.retried, r.redelivered, r.redispatched} {
		c.Add(ctx, 0)
	}
	r.relay.Add(ctx, 0, resultOK)
	r.relay.Add(ctx, 0, resultFail)
	return nil
}

var (
	resultOK   = metric.WithAttributeSet(attribute.NewSet(attribute.String("result", "ok")))
	resultFail = metric.WithAttributeSet(attribute.NewSet(attribute.String("result", "fail")))
)

func (r *Recorder) RecordDispatchWait(d time.Duration) {
	r.dispatchWait.Record(context.Background(), d.Seconds())
}

func (r *Recorder) RecordTransfer(d time.Duration) {
	r.transfer.Record(context.Background(), d.Seconds())
}

func (r *Recorder) IncCompleted()    { r.completed.Add(context.Background(), 1) }
func (r *Recorder) IncFailed()       { r.failed.Add(context.Background(), 1) }
func (r *Recorder) IncRetried()      { r.retried.Add(context.Background(), 1) }
func (r *Recorder) IncRedelivered()  { r.redelivered.Add(context.Background(), 1) }
func (r *Recorder) IncRedispatched() { r.redispatched.Add(context.Background(), 1) }

func (r *Recorder) RecordRelayPublish(ok bool) {
	if ok {
		r.relay.Add(context.Background(), 1, resultOK)
		return
	}
	r.relay.Add(context.Background(), 1, resultFail)
}

// RegisterOutboxGauge registers the observable outbox-backlog gauges. read is
// called at each scrape (with a one-second timeout) and returns the current
// unsent count and the age of the oldest unsent event. cmd wires it to the
// repository; a read error is logged by the OTel error handler, not fatal.
func (r *Recorder) RegisterOutboxGauge(read func(ctx context.Context) (unsent int64, oldestAgeSeconds float64, err error)) error {
	unsent, err := r.meter.Int64ObservableGauge("goload_outbox_unsent",
		metric.WithUnit("{event}"), metric.WithDescription("Unsent outbox events."))
	if err != nil {
		return err
	}
	oldest, err := r.meter.Float64ObservableGauge("goload_outbox_oldest_unsent",
		metric.WithUnit("s"), metric.WithDescription("Age of the oldest unsent outbox event."))
	if err != nil {
		return err
	}
	_, err = r.meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		n, age, rerr := read(ctx)
		if rerr != nil {
			return rerr
		}
		o.ObserveInt64(unsent, n)
		o.ObserveFloat64(oldest, age)
		return nil
	}, unsent, oldest)
	return err
}

// RegisterTasksByStatusGauge registers the observable tasks-by-status gauge.
// read is called at each scrape (with a one-second timeout) and returns the
// count for each status; cmd reports the full status enum (zeros included) so a
// status that drops to zero still appears.
func (r *Recorder) RegisterTasksByStatusGauge(read func(ctx context.Context) (map[string]int64, error)) error {
	g, err := r.meter.Int64ObservableGauge("goload_tasks_by_status",
		metric.WithUnit("{task}"), metric.WithDescription("Tasks by status."))
	if err != nil {
		return err
	}
	_, err = r.meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		counts, rerr := read(ctx)
		if rerr != nil {
			return rerr
		}
		for status, n := range counts {
			o.ObserveInt64(g, n, metric.WithAttributes(attribute.String("status", status)))
		}
		return nil
	}, g)
	return err
}

// MeterProvider exposes the provider so cmd can hand it to the otelgrpc/otelhttp
// contrib instrumentation (edge RPC/HTTP metrics).
func (r *Recorder) MeterProvider() metric.MeterProvider { return r.mp }

func (r *Recorder) Shutdown(ctx context.Context) error { return r.mp.Shutdown(ctx) }

// newResource identifies serviceName for both the meter and the tracer.
func newResource(serviceName string) *resource.Resource {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName))
	}
	return res
}
