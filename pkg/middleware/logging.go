package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoggingGRPCInterceptor intercepts gRPC requests to log them.
func LoggingGRPCInterceptor(logger log.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)

		if err != nil {
			errStatus, _ := status.FromError(err)
			code := errStatus.Code()
			
			if code == codes.Internal || code == codes.Unknown {
				level.Error(logger).Log(
					"method", info.FullMethod,
					"duration", duration,
					"err", err,
					"msg", "grpc request failed with internal error",
				)
			} else {
				level.Debug(logger).Log(
					"method", info.FullMethod,
					"duration", duration,
					"err", err,
					"code", code.String(),
					"msg", "grpc request failed with handled error",
				)
			}
		} else {
			level.Info(logger).Log(
				"method", info.FullMethod,
				"duration", duration,
				"msg", "grpc request handled successfully",
			)
		}

		return resp, err
	}
}

// responseWriter intercepts http responses to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// LoggingHTTPMiddleware returns a middleware that logs HTTP requests.
func LoggingHTTPMiddleware(logger log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			if rw.statusCode >= 500 {
				level.Error(logger).Log(
					"method", r.Method,
					"url", r.URL.String(),
					"status", rw.statusCode,
					"duration", duration,
					"msg", "http request failed with server error",
				)
			} else if rw.statusCode >= 400 {
				level.Debug(logger).Log(
					"method", r.Method,
					"url", r.URL.String(),
					"status", rw.statusCode,
					"duration", duration,
					"msg", "http request failed with client error",
				)
			} else {
				level.Info(logger).Log(
					"method", r.Method,
					"url", r.URL.String(),
					"status", rw.statusCode,
					"duration", duration,
					"msg", "http request handled successfully",
				)
			}
		})
	}
}
