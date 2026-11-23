package observability

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// otelhttp fills server.address/server.port from the request Host header, which
// is client-controlled. The provider must drop those so a public /metrics
// cannot be grown without bound (remote memory exhaustion).
func TestHTTPServerMetricsDropClientControlledLabels(t *testing.T) {
	rec, h, err := NewRecorder("test")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer func() { _ = rec.Shutdown(context.Background()) }()

	hist, err := rec.meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"))
	if err != nil {
		t.Fatalf("histogram: %v", err)
	}
	for i := 0; i < 50; i++ {
		hist.Record(context.Background(), 0.01, metric.WithAttributes(
			attribute.String("server.address", fmt.Sprintf("h%d.attacker.example", i)),
			attribute.Int("server.port", 1000+i),
		))
	}

	body := scrapeMetrics(t, h)
	if strings.Contains(body, "server_address") || strings.Contains(body, "server_port") {
		t.Fatalf("client-controlled host labels leaked into /metrics:\n%s", body)
	}
	if n := strings.Count(body, "http_server_request_duration_seconds_count"); n > 2 {
		t.Fatalf("host labels not dropped: %d series (want them collapsed)", n)
	}
}

func TestRecorderExposesMetricNamesWithoutHighCardinalityLabels(t *testing.T) {
	rec, h, err := NewRecorder("test")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer func() { _ = rec.Shutdown(context.Background()) }()

	if rec.MeterProvider() == nil {
		t.Fatal("MeterProvider must be non-nil for otelgrpc/otelhttp wiring")
	}

	rec.RecordDispatchWait(2 * time.Second)
	rec.RecordTransfer(time.Second)
	rec.IncCompleted()
	rec.IncFailed()
	rec.IncRetried()
	rec.IncRedelivered()
	rec.IncRedispatched()
	rec.RecordRelayPublish(true)
	rec.RecordRelayPublish(false)

	body := scrapeMetrics(t, h)

	for _, want := range []string{
		"goload_dispatch_wait_seconds",
		"goload_transfer_duration_seconds",
		"goload_tasks_completed_total",
		"goload_tasks_failed_total",
		"goload_tasks_retried_total",
		"goload_redelivered_total",
		"goload_reconciler_redispatched_total",
		"goload_relay_published_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metric %q missing from /metrics output:\n%s", want, body)
		}
	}
	for _, bad := range []string{"task_id", "worker_id", "url=", "otel_scope_name"} {
		if strings.Contains(body, bad) {
			t.Fatalf("unexpected/high-cardinality label %q leaked into /metrics", bad)
		}
	}
	if !strings.Contains(body, `goload_relay_published_total{result="fail"}`) {
		t.Fatalf("relay publish result label missing:\n%s", body)
	}
}

func scrapeMetrics(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
