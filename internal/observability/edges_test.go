package observability

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func newTestRecorder(t *testing.T) *Recorder {
	t.Helper()
	rec, _, err := NewRecorder("test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rec.Shutdown(context.Background()) })
	return rec
}

// The public edge starts its own trace: a client-sent traceparent can neither
// choose the trace ID nor force sampling.
func TestPublicHTTPHandler_StartsANewTraceWhateverTheClientSends(t *testing.T) {
	tr, rec := newRecordingTracer(t)
	h := PublicHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), newTestRecorder(t), tr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].SpanContext().TraceID().String() == "0af7651916cd43dd8448eb211c80319c" {
		t.Fatal("the gateway adopted the client's trace ID")
	}
	if spans[0].Parent().IsValid() {
		t.Fatal("the gateway span must be a root")
	}
}

// gorilla/mux does not set Request.Pattern, so the span is renamed after
// routing to the route template - never the raw path.
func TestRouteSpanNames_NameTheSpanAfterTheRouteTemplate(t *testing.T) {
	tr, rec := newRecordingTracer(t)
	r := mux.NewRouter()
	r.Use(RouteSpanNames)
	r.HandleFunc("/api/v1/tasks/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	h := PublicHTTPHandler(r, newTestRecorder(t), tr)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tasks/123", nil))

	if got := rec.Ended()[0].Name(); got != "GET /api/v1/tasks/{id}" {
		t.Fatalf("span name = %q, want the route template", got)
	}
}

// The client handler must carry the trace into gRPC metadata with the tracer's
// own propagator (the global one is a no-op).
func TestGRPCClientHandler_CarriesTheTraceToTheServer(t *testing.T) {
	tr, _ := newRecordingTracer(t)
	rec := newTestRecorder(t)

	seen := make(chan string, 1)
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.StatsHandler(GRPCServerHandler(rec, tr)),
		grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			if v := md.Get("traceparent"); len(v) > 0 {
				seen <- v[0]
			} else {
				seen <- ""
			}
			return next(ctx, req)
		}),
	)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(GRPCClientHandler(rec, tr)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, span := tr.tracer.Start(context.Background(), "caller")
	defer span.End()
	if _, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatal(err)
	}

	got := <-seen
	want := trace.SpanContextFromContext(ctx).TraceID().String()
	if len(got) < 35 || got[3:35] != want {
		t.Fatalf("server saw traceparent %q, want trace %s", got, want)
	}
}

// Go's server accepts any method token; an unknown one must not mint a span name.
func TestPublicHTTPHandler_NamesUnknownMethodsHTTP(t *testing.T) {
	tr, rec := newRecordingTracer(t)
	r := mux.NewRouter()
	r.Use(RouteSpanNames)
	r.HandleFunc("/docs", func(http.ResponseWriter, *http.Request) {})
	h := PublicHTTPHandler(r, newTestRecorder(t), tr)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("ZZATTACKER1", "/nope/12345", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("ZZATTACKER2", "/docs", nil))

	spans := rec.Ended()
	require.Len(t, spans, 2)
	require.Equal(t, "HTTP", spans[0].Name())
	require.Equal(t, "HTTP /docs", spans[1].Name())
}

func TestPublicHTTPHandler_DoesNotTraceHealthChecks(t *testing.T) {
	tr, rec := newRecordingTracer(t)
	h := PublicHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), newTestRecorder(t), tr)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Empty(t, rec.Ended())
}

// Client baggage is not trusted: it would be stored on the outbox row and sent
// to every downstream service.
func TestPublicHTTPHandler_DropsClientBaggage(t *testing.T) {
	tr, _ := newRecordingTracer(t)
	var carrier map[string]string
	h := PublicHTTPHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		carrier = tr.Inject(r.Context())
	}), newTestRecorder(t), tr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/create", nil)
	req.Header.Set("baggage", "attacker=controlled")
	h.ServeHTTP(httptest.NewRecorder(), req)

	require.NotEmpty(t, carrier["traceparent"])
	require.NotContains(t, carrier, "baggage")
}
