package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime/debug"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryHTTPMiddleware converts panics in downstream handlers into 500 responses
// and logs the panic details with stack trace for diagnostics.
func RecoveryHTTPMiddleware(logger log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					reqID := RequestIDFromContext(r.Context())
					level.Error(logger).Log(
						"method", r.Method,
						"url", r.URL.String(),
						"request_id", reqID,
						"panic", rec,
						"stack", string(debug.Stack()),
						"msg", "http handler panic recovered",
					)

					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					body := map[string]any{"error": "internal server error"}
					if reqID != "" {
						body["request_id"] = reqID
					}
					_ = json.NewEncoder(w).Encode(body)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryGRPCInterceptor converts panics in unary handlers into codes.Internal
// and logs the panic details with stack trace for diagnostics.
func RecoveryGRPCInterceptor(logger log.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) { //nolint:nonamedreturns // named results required so recover can return an error
		defer func() {
			if rec := recover(); rec != nil {
				level.Error(logger).Log(
					"method", info.FullMethod,
					"panic", rec,
					"stack", string(debug.Stack()),
					"msg", "grpc handler panic recovered",
				)
				resp = nil
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}
