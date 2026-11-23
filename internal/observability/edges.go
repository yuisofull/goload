package observability

import (
	"net/http"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/stats"
)

// GRPCServerHandler instruments a gRPC server with spans and metrics. It uses
// the tracer's provider and propagator explicitly (the globals stay untouched)
// and skips health checks.
func GRPCServerHandler(rec *Recorder, tr *Tracer) stats.Handler {
	return otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(tr.TracerProvider()),
		otelgrpc.WithMeterProvider(rec.MeterProvider()),
		otelgrpc.WithPropagators(tr.Propagator()),
		otelgrpc.WithFilter(filters.Not(filters.HealthCheck())),
	)
}

// GRPCClientHandler instruments a gRPC client connection and carries the trace
// to the server in the call metadata.
func GRPCClientHandler(rec *Recorder, tr *Tracer) stats.Handler {
	return otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(tr.TracerProvider()),
		otelgrpc.WithMeterProvider(rec.MeterProvider()),
		otelgrpc.WithPropagators(tr.Propagator()),
	)
}

// PublicHTTPHandler instruments the public HTTP edge. It always starts a new
// trace, so a client-sent traceparent cannot choose the trace ID or force
// sampling; it names spans by method (RouteSpanNames refines that), never by
// the raw path, and skips /health.
func PublicHTTPHandler(h http.Handler, rec *Recorder, tr *Tracer) http.Handler {
	return otelhttp.NewHandler(h, "http",
		otelhttp.WithTracerProvider(tr.TracerProvider()),
		otelhttp.WithMeterProvider(rec.MeterProvider()),
		otelhttp.WithPropagators(tr.Propagator()),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/health" }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if r.Pattern != "" {
				return r.Pattern
			}
			return spanMethod(r.Method)
		}),
	)
}

// spanMethod is the method for a span name: any method outside the standard
// set is "HTTP", so a client cannot mint span names.
func spanMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	}
	return "HTTP"
}

// RouteSpanNames is gorilla/mux middleware that renames the request's span to
// "METHOD /route/{template}" once a route matched; gorilla does not set
// Request.Pattern, so the span would otherwise carry only the method.
func RouteSpanNames(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route := mux.CurrentRoute(r); route != nil {
			if template, err := route.GetPathTemplate(); err == nil {
				trace.SpanFromContext(r.Context()).SetName(spanMethod(r.Method) + " " + template)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// LogErrors routes OTel's internal errors (a failed span export, a failing
// gauge read) to log instead of the standard library logger.
func LogErrors(log func(error)) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(log))
}
