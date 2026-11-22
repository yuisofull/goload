package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsMuxServesMetricsAnd404sElse(t *testing.T) {
	_, h, err := NewRecorder("test")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	mux := MetricsMux(h)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if code := statusOf(t, srv.URL+"/metrics"); code != http.StatusOK {
		t.Fatalf("GET /metrics: want 200, got %d", code)
	}
	if code := statusOf(t, srv.URL+"/"); code != http.StatusNotFound {
		t.Fatalf("GET /: want 404, got %d", code)
	}
}

func statusOf(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}
