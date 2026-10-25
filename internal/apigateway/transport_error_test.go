package apigateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/pkg/middleware"
)

func TestEncodeError_MapsDomainErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "not found",
			err:        &errors.Error{Code: errors.ErrCodeNotFound, Message: "task not found"},
			wantStatus: http.StatusNotFound,
			wantMsg:    "task not found",
		},
		{
			name:       "invalid input",
			err:        &errors.Error{Code: errors.ErrCodeInvalidInput, Message: "SourceURL is required"},
			wantStatus: http.StatusBadRequest,
			wantMsg:    "SourceURL is required",
		},
		{
			name:       "unauthenticated",
			err:        &errors.Error{Code: errors.ErrCodeUnauthenticated, Message: "missing or invalid token"},
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "missing or invalid token",
		},
		{
			name:       "invalid password",
			err:        &errors.Error{Code: auth.ErrCodeInvalidPassword, Message: "invalid password"},
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "invalid password",
		},
		{
			name:       "conflict",
			err:        &errors.Error{Code: errors.ErrCodeConflict, Message: "task already running"},
			wantStatus: http.StatusConflict,
			wantMsg:    "task already running",
		},
		{
			name:       "invalid state",
			err:        &errors.Error{Code: errors.ErrCodeInvalidState, Message: "cannot pause completed task"},
			wantStatus: http.StatusPreconditionFailed,
			wantMsg:    "cannot pause completed task",
		},
		{
			name:       "internal sanitizes message",
			err:        &errors.Error{Code: errors.ErrCodeInternal, Message: "sql boom", Cause: assert.AnError},
			wantStatus: http.StatusInternalServerError,
			wantMsg:    "internal server error",
		},
		{
			name:       "grpc not found",
			err:        status.Error(codes.NotFound, "task not found"),
			wantStatus: http.StatusNotFound,
			wantMsg:    "task not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			ctx := middleware.ContextWithRequestID(t.Context(), "req_test_1")
			encodeError(ctx, tt.err, rec)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantMsg, body["error"])
			assert.Equal(t, "req_test_1", body["request_id"])
			assert.NotContains(t, rec.Body.String(), "sql")
		})
	}
}

func TestAddTokenToContext_MissingHeaderJSON(t *testing.T) {
	t.Parallel()

	handler := addTokenToContext(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/list", nil)
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "missing authorization header", body["error"])
}
