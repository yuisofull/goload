package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// RecoveryHTTPMiddleware converts panics in downstream handlers into 500 responses
// and logs the panic details with stack trace for diagnostics.
func RecoveryHTTPMiddleware(logger log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					level.Error(logger).Log(
						"method", r.Method,
						"url", r.URL.String(),
						"panic", rec,
						"stack", string(debug.Stack()),
						"msg", "http handler panic recovered",
					)

					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error"}`))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
