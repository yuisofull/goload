package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const (
	// RequestIDHeader is the canonical HTTP header used to propagate request IDs.
	RequestIDHeader = "X-Request-Id"
	maxRequestIDLen = 128
)

type contextKey int

const requestIDKey contextKey = iota

// ContextWithRequestID returns a child context carrying requestID.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext returns the request ID stored in ctx, or "".
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RequestIDHTTPMiddleware propagates or generates an X-Request-Id and stores it
// in the request context so logs and error responses can correlate a failure.
func RequestIDHTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = uuid.NewString()
		}

		w.Header().Set(RequestIDHeader, id)
		ctx := ContextWithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sanitizeRequestID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= maxRequestIDLen {
			break
		}
	}
	return b.String()
}
