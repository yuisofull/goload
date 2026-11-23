package task

import "context"

// Tracing carries a download's trace across the outbox. The OTel-backed
// implementation is wired in cmd; nopTracing is the default, so core needs no
// tracing backend (CC-2). Carriers are opaque string maps (W3C traceparent and
// tracestate).
type Tracing interface {
	// Inject returns the current trace context as a carrier to persist with an
	// outbox row; nil when ctx carries no span.
	Inject(ctx context.Context) map[string]string
	// StartPublish starts the outbox publish span, parented on the carrier
	// stored with the row (a new trace when it is nil), and returns the
	// carrier to send with the message plus a func that ends the span.
	StartPublish(ctx context.Context, carrier map[string]string, topic, messageID string) (map[string]string, func(error))
}

type nopTracing struct{}

func (nopTracing) Inject(context.Context) map[string]string { return nil }

func (nopTracing) StartPublish(context.Context, map[string]string, string, string) (map[string]string, func(error)) {
	return nil, func(error) {}
}

// WithTracing injects the tracing used to capture the trace context of the
// outbox rows the service writes. A nil value keeps the no-op default.
func WithTracing(t Tracing) ServiceOption {
	return func(s *service) {
		if t != nil {
			s.tracing = t
		}
	}
}

// WithOutboxRelayTracing injects the tracing the relay uses for its publish
// spans. A nil value keeps the no-op default.
func WithOutboxRelayTracing(t Tracing) OutboxRelayOption {
	return func(r *OutboxRelay) {
		if t != nil {
			r.tracing = t
		}
	}
}

// WithAttemptTracing injects the tracing used to capture the trace context of
// the lifecycle outbox rows. A nil value keeps the no-op default.
func WithAttemptTracing(t Tracing) AttemptServiceOption {
	return func(s *AttemptService) {
		if t != nil {
			s.tracing = t
		}
	}
}
