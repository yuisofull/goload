package apigateway

import (
	"context"
	"encoding/json"
	"github.com/go-kit/kit/transport"
	"github.com/go-kit/log/level"
	"github.com/yuisofull/goload/internal/file"
	"net/http"
	"strconv"
	"strings"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/go-kit/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type HTTPCreateDownloadTaskRequest struct {
	DownloadType int    `json:"download_type"`
	URL          string `json:"url"`
}

type HTTPCreateDownloadTaskResponse struct {
	DownloadTask *HTTPDownloadTask `json:"download_task"`
}

type HTTPGetDownloadTaskListRequest struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
}

type HTTPGetDownloadTaskListResponse struct {
	DownloadTasks []*HTTPDownloadTask `json:"download_tasks"`
	TotalCount    uint64              `json:"total_count"`
}

type HTTPUpdateDownloadTaskRequest struct {
	DownloadTaskID uint64 `json:"download_task_id"`
	URL            string `json:"url"`
}

type HTTPUpdateDownloadTaskResponse struct {
	DownloadTask *HTTPDownloadTask `json:"download_task"`
}

type HTTPDeleteDownloadTaskRequest struct {
	DownloadTaskID uint64 `json:"download_task_id"`
}

type HTTPDeleteDownloadTaskResponse struct{}

type HTTPDownloadTask struct {
	ID             uint64 `json:"id"`
	OfAccountID    uint64 `json:"of_account_id"`
	DownloadType   int    `json:"download_type"`
	URL            string `json:"url"`
	DownloadStatus int    `json:"download_status"`
}

// NewHTTPHandler creates HTTP handlers for all gateway endpoints
func NewHTTPHandler(endpoints GatewayEndpoints, logger log.Logger) http.Handler {
	options := []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(encodeError),
		httptransport.ServerErrorHandler(transport.NewLogErrorHandler(level.Error(logger))),
	}

	mux := http.NewServeMux()

	// Download Task Service endpoints
	createHandler := httptransport.NewServer(
		endpoints.CreateDownloadTask,
		decodeHTTPCreateDownloadTaskRequest,
		encodeHTTPResponse,
		options...,
	)
	mux.Handle("/api/v1/download-tasks", addTokenToContext(createHandler))

	listHandler := httptransport.NewServer(
		endpoints.GetDownloadTaskList,
		decodeHTTPGetDownloadTaskListRequest,
		encodeHTTPResponse,
		options...,
	)
	mux.Handle("/api/v1/download-tasks/list", addTokenToContext(listHandler))

	updateHandler := httptransport.NewServer(
		endpoints.UpdateDownloadTask,
		decodeHTTPUpdateDownloadTaskRequest,
		encodeHTTPResponse,
		options...,
	)
	mux.Handle("/api/v1/download-tasks/update", addTokenToContext(updateHandler))

	deleteHandler := httptransport.NewServer(
		endpoints.DeleteDownloadTask,
		decodeHTTPDeleteDownloadTaskRequest,
		encodeHTTPResponse,
		options...,
	)
	mux.Handle("/api/v1/download-tasks/delete", addTokenToContext(deleteHandler))

	// Future service endpoints can be added here:
	// Auth Service endpoints
	// mux.Handle("/api/v1/auth/login", loginHandler)
	// mux.Handle("/api/v1/auth/register", registerHandler)

	// File Service endpoints
	// mux.Handle("/api/v1/files/{id}", addTokenToContext(getFileHandler))

	return mux
}

// addTokenToContext extracts JWT token from HTTP Authorization header and adds it to context
func addTokenToContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		// Check for Bearer token format
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, bearerPrefix)
		if token == "" {
			http.Error(w, "Missing token", http.StatusUnauthorized)
			return
		}

		// Add token to request context
		ctx := context.WithValue(r.Context(), tokenKey, token)
		r = r.WithContext(ctx)

		// Call next handler
		next.ServeHTTP(w, r)
	})
}

// HTTP request decoders
func decodeHTTPCreateDownloadTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req HTTPCreateDownloadTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		return nil, http.ErrNoCookie // This should not happen if middleware works correctly
	}

	return &CreateDownloadTaskRequest{
		UserID:       userID,
		DownloadType: file.DownloadType(req.DownloadType),
		URL:          req.URL,
	}, nil
}

func decodeHTTPGetDownloadTaskListRequest(_ context.Context, r *http.Request) (interface{}, error) {
	offsetStr := r.URL.Query().Get("offset")
	limitStr := r.URL.Query().Get("limit")

	var offset, limit uint64
	var err error

	if offsetStr != "" {
		offset, err = strconv.ParseUint(offsetStr, 10, 64)
		if err != nil {
			return nil, err
		}
	}

	if limitStr != "" {
		limit, err = strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			return nil, err
		}
	} else {
		limit = 10 // default limit
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		return nil, http.ErrNoCookie
	}

	return &GetDownloadTaskListRequest{
		UserID: userID,
		Offset: offset,
		Limit:  limit,
	}, nil
}

func decodeHTTPUpdateDownloadTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req HTTPUpdateDownloadTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		return nil, http.ErrNoCookie
	}

	return &UpdateDownloadTaskRequest{
		UserID:         userID,
		DownloadTaskID: req.DownloadTaskID,
		URL:            req.URL,
	}, nil
}

func decodeHTTPDeleteDownloadTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req HTTPDeleteDownloadTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		return nil, http.ErrNoCookie
	}

	return &DeleteDownloadTaskRequest{
		UserID:         userID,
		DownloadTaskID: req.DownloadTaskID,
	}, nil
}

// HTTP response encoder
func encodeHTTPResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(response)
}

// HTTP error encoder
func encodeError(_ context.Context, err error, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	var statusCode int

	if grpcStatus, ok := status.FromError(err); ok {
		switch grpcStatus.Code() {
		case codes.NotFound:
			statusCode = http.StatusNotFound
		case codes.InvalidArgument:
			statusCode = http.StatusBadRequest
		case codes.Unauthenticated:
			statusCode = http.StatusUnauthorized
		case codes.PermissionDenied:
			statusCode = http.StatusForbidden
		case codes.AlreadyExists:
			statusCode = http.StatusConflict
		case codes.Internal:
			statusCode = http.StatusInternalServerError
		case codes.Unavailable:
			statusCode = http.StatusServiceUnavailable
		case codes.DeadlineExceeded:
			statusCode = http.StatusRequestTimeout
		case codes.ResourceExhausted:
			statusCode = http.StatusTooManyRequests
		case codes.FailedPrecondition:
			statusCode = http.StatusPreconditionFailed
		case codes.Aborted:
			statusCode = http.StatusConflict
		case codes.OutOfRange:
			statusCode = http.StatusBadRequest
		case codes.Unimplemented:
			statusCode = http.StatusNotImplemented
		case codes.Unknown:
			statusCode = http.StatusInternalServerError
		default:
			statusCode = http.StatusInternalServerError
		}
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": err.Error(),
	})
}
