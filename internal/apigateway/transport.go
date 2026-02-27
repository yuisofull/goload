package apigateway

import (
	"context"
	"encoding/json"
	"github.com/go-kit/kit/transport"
	"github.com/go-kit/log/level"
	"net/http"
	"strconv"
	"strings"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/go-kit/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

	{
		createHandler := httptransport.NewServer(
			endpoints.AuthCreateEndpoint,
			func(_ context.Context, r *http.Request) (interface{}, error) {
				var req CreateAccountGatewayRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					return nil, err
				}
				return &req, nil
			},
			encodeHTTPResponse,
			options...,
		)
		mux.Handle("/api/v1/auth/create", createHandler)
	}

	{
		sessionHandler := httptransport.NewServer(
			endpoints.AuthSessionEndpoint,
			func(_ context.Context, r *http.Request) (interface{}, error) {
				var req CreateSessionGatewayRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					return nil, err
				}
				return &req, nil
			},
			encodeHTTPResponse,
			options...,
		)
		mux.Handle("/api/v1/auth/session", sessionHandler)
	}

	return mux
}

// NewHTTPHandlerWithDownload builds the same handlers as NewHTTPHandler and also
// registers a /download handler that consumes tokens from the provided
// TokenStore and streams files from the provided storage backend.
func NewHTTPHandlerWithDownload(endpoints GatewayEndpoints, logger log.Logger, store storage.Reader, tokenStore task.TokenStore) http.Handler {
	// call NewHTTPHandler which will pick up auth endpoints from the endpoints struct
	mux := NewHTTPHandler(endpoints, logger).(*http.ServeMux)

	// Register download handler
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		// Consume token
		meta, err := tokenStore.ConsumeToken(ctx, token)
		if err != nil {
			level.Error(logger).Log("msg", "failed to consume token", "err", err)
			http.Error(w, "failed to validate token", http.StatusInternalServerError)
			return
		}
		if meta == nil {
			level.Info(logger).Log("msg", "token not found or expired", "token", token)
			http.Error(w, "invalid or expired token", http.StatusNotFound)
			return
		}
		if time.Now().After(meta.Expires) {
			level.Info(logger).Log("msg", "token expired", "token", token)
			http.Error(w, "token expired", http.StatusNotFound)
			return
		}

		// ACL: ensure authenticated user matches token owner
		userID, ok := UserIDFromContext(r.Context())
		if !ok {
			level.Warn(logger).Log("msg", "download request missing user in context")
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		if meta.OwnerID != 0 && meta.OwnerID != userID {
			level.Warn(logger).Log("msg", "token owner mismatch", "token_owner", meta.OwnerID, "request_user", userID)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		level.Info(logger).Log("msg", "token validated", "task_key", meta.Key, "owner", meta.OwnerID)

		// Support Range header
		var reader io.ReadCloser
		rangeHdr := r.Header.Get("Range")
		if rangeHdr != "" {
			// expect format: bytes=start-end or bytes=start-
			if !strings.HasPrefix(rangeHdr, "bytes=") {
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			parts := strings.Split(strings.TrimPrefix(rangeHdr, "bytes="), "-")
			if len(parts) != 2 {
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			start, err1 := strconv.ParseInt(parts[0], 10, 64)
			var end int64 = -1
			var err2 error
			if parts[1] != "" {
				end, err2 = strconv.ParseInt(parts[1], 10, 64)
			}
			if err1 != nil || (parts[1] != "" && err2 != nil) {
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if end >= 0 {
				reader, err = store.GetWithRange(ctx, meta.Key, start, end)
			} else {
				reader, err = store.GetWithRange(ctx, meta.Key, start, -1)
			}
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to fetch range: %v", err), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		} else {
			reader, err = store.Get(ctx, meta.Key)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to fetch file: %v", err), http.StatusInternalServerError)
				return
			}
		}
		defer reader.Close()

		// Try to get info for headers
		if info, err := store.GetInfo(ctx, meta.Key); err == nil && info != nil {
			if info.ContentType != "" {
				w.Header().Set("Content-Type", info.ContentType)
			}
			if info.FileSize > 0 {
				w.Header().Set("Content-Length", strconv.FormatInt(info.FileSize, 10))
			}
			if info.FileName != "" {
				w.Header().Set("Content-Disposition", "attachment; filename=\""+info.FileName+"\"")
			}
		}

		// Stream the content
		if _, err := io.Copy(w, reader); err != nil {
			// If client aborted, nothing to do; otherwise log error
			http.Error(w, "failed to stream file", http.StatusInternalServerError)
			return
		}
	})

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
