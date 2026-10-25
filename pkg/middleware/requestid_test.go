package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/pkg/middleware"
)

func TestRequestIDHTTPMiddleware_GeneratesAndPropagates(t *testing.T) {
	t.Parallel()

	var gotID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = middleware.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	handler := middleware.RequestIDHTTPMiddleware(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	require.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
	assert.Equal(t, rec.Header().Get(middleware.RequestIDHeader), gotID)
}

func TestRequestIDHTTPMiddleware_UsesIncomingHeader(t *testing.T) {
	t.Parallel()

	var gotID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = middleware.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	handler := middleware.RequestIDHTTPMiddleware(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(middleware.RequestIDHeader, "client-req-1")
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "client-req-1", rec.Header().Get(middleware.RequestIDHeader))
	assert.Equal(t, "client-req-1", gotID)
}
