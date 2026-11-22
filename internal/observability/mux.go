package observability

import "net/http"

// MetricsMux serves the Prometheus handler at GET /metrics and nothing else.
// cmd entry points mount it on a dedicated admin HTTP server (task, auth) or on
// an existing one (download, gateway, pocket).
func MetricsMux(h http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", h)
	return mux
}
