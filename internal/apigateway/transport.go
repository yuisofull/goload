package apigateway

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/kit/transport"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/go-kit/log"
	"github.com/samber/lo"
	"github.com/yuisofull/goload/docs"
	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/internal/task"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type HTTPListTasksRequest struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
}

type HTTPListTasksResponse struct {
	Tasks      []*HTTPTask `json:"download_tasks"`
	TotalCount uint64      `json:"total_count"`
}

type HTTPTask struct {
	ID              uint64         `json:"id,omitempty"`
	OfAccountID     uint64         `json:"of_account_id,omitempty"`
	FileName        string         `json:"file_name,omitempty"`
	SourceUrl       string         `json:"source_url,omitempty"`
	SourceType      string         `json:"source_type,omitempty"`
	ChecksumType    *string        `json:"checksum_type,omitempty"`
	ChecksumValue   *string        `json:"checksum_value,omitempty"`
	Status          string         `json:"status,omitempty"`
	Progress        *float64       `json:"progress,omitempty"`
	DownloadedBytes *int64         `json:"downloaded_bytes,omitempty"`
	TotalBytes      *int64         `json:"total_bytes,omitempty"`
	ErrorMessage    *string        `json:"error_message,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       *time.Time     `json:"created_at,omitempty"`
	UpdatedAt       *time.Time     `json:"updated_at,omitempty"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
}

// NewHTTPHandler creates HTTP handlers for all gateway endpoints.
// If authCreate and authSession endpoints are non-nil, register auth handlers.
func NewHTTPHandler(endpoints GatewayEndpoints, logger log.Logger) http.Handler {
	options := []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(encodeError),
		httptransport.ServerErrorHandler(transport.NewLogErrorHandler(level.Error(logger))),
	}



	r := mux.NewRouter()

	// --- health ---------------------------------------------------------
	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}).Methods(http.MethodGet)

	// --- /docs ----------------------------------------------------------
	sub, _ := fs.Sub(docs.FS, ".")
	r.PathPrefix("/docs/").Handler(http.StripPrefix("/docs/", http.FileServer(http.FS(sub))))

	// --- /api/v1/tasks --------------------------------------------------
	tasks := r.PathPrefix("/api/v1/tasks").Subrouter()

	tasks.Handle("/list", addTokenToContext(httptransport.NewServer(
		endpoints.ListTasksEndpoint,
		decodeHTTPListTaskRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodGet)

	tasks.Handle("/create", addTokenToContext(httptransport.NewServer(
		endpoints.CreateTaskEndpoint,
		decodeHTTPCreateRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodPost)

	tasks.Handle("/get", addTokenToContext(httptransport.NewServer(
		endpoints.GetTaskEndpoint,
		decodeHTTPGetRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodGet)

	tasks.Handle("/delete", addTokenToContext(httptransport.NewServer(
		endpoints.DeleteTaskEndpoint,
		decodeHTTPIDRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodDelete)

	tasks.Handle("/pause", addTokenToContext(httptransport.NewServer(
		endpoints.PauseTaskEndpoint,
		decodeHTTPPauseTaskRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodPost)

	tasks.Handle("/resume", addTokenToContext(httptransport.NewServer(
		endpoints.ResumeTaskEndpoint,
		decodeHTTPResumeTaskRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodPost)

	tasks.Handle("/cancel", addTokenToContext(httptransport.NewServer(
		endpoints.CancelTaskEndpoint,
		decodeHTTPCancelTaskRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodPost)

	tasks.Handle("/retry", addTokenToContext(httptransport.NewServer(
		endpoints.RetryTaskEndpoint,
		decodeHTTPRetryTaskRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodPost)

	tasks.Handle("/exists", addTokenToContext(httptransport.NewServer(
		endpoints.CheckFileExistsEndpoint,
		decodeHTTPCheckFileExistsRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodGet)

	tasks.Handle("/progress", addTokenToContext(httptransport.NewServer(
		endpoints.GetTaskProgressEndpoint,
		decodeHTTPGetTaskProgressRequest,
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodGet)

	tasks.Handle("/download-url", addTokenToContext(httptransport.NewServer(
		endpoints.GenerateDownloadURLEndpoint,
		func(_ context.Context, r *http.Request) (interface{}, error) {
			var req GenerateDownloadURLRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				return nil, err
			}
			return &req, nil
		},
		encodeHTTPResponse,
		options...,
	))).Methods(http.MethodPost)

	// --- /api/v1/auth ---------------------------------------------------
	auth := r.PathPrefix("/api/v1/auth").Subrouter()

	auth.Handle("/create", httptransport.NewServer(
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
	)).Methods(http.MethodPost)

	auth.Handle("/session", httptransport.NewServer(
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
	)).Methods(http.MethodPost)

	return r
}

// NewHTTPHandlerWithDownload extends NewHTTPHandler with a /download endpoint
// that acts as the server-side fallback for when a presigned URL is unavailable.
// Clients that received direct=false from /tasks/download-url hit this endpoint
// with ?token=<token>. It validates+consumes the token and streams the file bytes
// from the storage backend — supporting Range requests.
func NewHTTPHandlerWithDownload(
	endpoints GatewayEndpoints,
	logger log.Logger,
	store storage.Reader,
	tokenStore task.TokenStore,
) *mux.Router {
	r := NewHTTPHandler(endpoints, logger).(*mux.Router)

	r.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		// Validate and consume the one-time token
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

		level.Info(logger).Log("msg", "token validated", "task_key", meta.Key, "owner", meta.OwnerID)

		// Set response headers from storage metadata
		if info, err := store.GetInfo(ctx, meta.Key); err == nil && info != nil {
			if info.ContentType != "" {
				w.Header().Set("Content-Type", info.ContentType)
			}
			if info.FileSize > 0 {
				w.Header().Set("Content-Length", strconv.FormatInt(info.FileSize, 10))
			}
			if info.FileName != "" {
				w.Header().Set("Content-Disposition", `attachment; filename="`+info.FileName+`"`)
			}
		}

		// Support Range header
		var reader io.ReadCloser
		rangeHdr := r.Header.Get("Range")
		if rangeHdr != "" {
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
			reader, err = store.GetWithRange(ctx, meta.Key, start, end)
			if err != nil {
				http.Error(w, "failed to fetch range: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		} else {
			reader, err = store.Get(ctx, meta.Key)
			if err != nil {
				http.Error(w, "failed to fetch file: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		defer reader.Close()

		if _, err := io.Copy(w, reader); err != nil {
			level.Warn(logger).Log("msg", "stream interrupted", "err", err)
		}
	}).Methods(http.MethodGet)

	return r
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

		ctx := context.WithValue(r.Context(), tokenKey, token)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

func decodeHTTPListTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
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
	}

	// NOTE: authentication is handled by endpoint middleware. Decoders run
	// before middleware, so don't rely on context having the user ID here.
	// We return a request without the filter populated; the endpoint will
	// populate the OfAccountID from the context once the auth middleware
	// has run.
	return &ListTasksRequest{
		Offset: lo.ToPtr(offset),
		Limit:  lo.ToPtr(limit),
	}, nil
}

func decodeHTTPCreateRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func decodeHTTPGetRequest(_ context.Context, r *http.Request) (interface{}, error) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		return nil, http.ErrMissingFile
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, err
	}
	return &GetTaskRequest{Id: id}, nil
}

func decodeHTTPIDRequest(_ context.Context, r *http.Request) (interface{}, error) {
	return decodeHTTPIDRequestName("id")(context.Background(), r)
}

func decodeHTTPPauseTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
	id, err := decodeHTTPQueryUint64(r, "id")
	if err != nil {
		return nil, err
	}
	return &PauseTaskRequest{Id: id}, nil
}

func decodeHTTPResumeTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
	id, err := decodeHTTPQueryUint64(r, "id")
	if err != nil {
		return nil, err
	}
	return &ResumeTaskRequest{Id: id}, nil
}

func decodeHTTPCancelTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
	id, err := decodeHTTPQueryUint64(r, "id")
	if err != nil {
		return nil, err
	}
	return &CancelTaskRequest{Id: id}, nil
}

func decodeHTTPRetryTaskRequest(_ context.Context, r *http.Request) (interface{}, error) {
	id, err := decodeHTTPQueryUint64(r, "id")
	if err != nil {
		return nil, err
	}
	return &RetryTaskRequest{Id: id}, nil
}

func decodeHTTPCheckFileExistsRequest(_ context.Context, r *http.Request) (interface{}, error) {
	id, err := decodeHTTPQueryUint64(r, "task_id")
	if err != nil {
		return nil, err
	}
	return &CheckFileExistsRequest{TaskId: id}, nil
}

func decodeHTTPGetTaskProgressRequest(_ context.Context, r *http.Request) (interface{}, error) {
	id, err := decodeHTTPQueryUint64(r, "task_id")
	if err != nil {
		return nil, err
	}
	return &GetTaskProgressRequest{TaskId: id}, nil
}

func decodeHTTPQueryUint64(r *http.Request, name string) (uint64, error) {
	idStr := r.URL.Query().Get(name)
	if idStr == "" {
		return 0, http.ErrMissingFile
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func decodeHTTPIDRequestName(name string) func(ctx context.Context, r *http.Request) (interface{}, error) {
	return func(_ context.Context, r *http.Request) (interface{}, error) {
		id, err := decodeHTTPQueryUint64(r, name)
		if err != nil {
			return nil, err
		}
		return &DeleteTaskRequest{Id: id}, nil
	}
}

func encodeHTTPResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(response)
}

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

	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": err.Error(),
	})
}
