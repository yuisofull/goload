package download

import "context"

// Tracing continues a download's trace on the worker. The OTel-backed
// implementation is wired in cmd; nopTracing is the default, so core needs no
// tracing backend (CC-2).
type Tracing interface {
	// StartConsume continues the trace carried by a dispatch message (its
	// metadata is the carrier) and starts the span for processing it. The
	// returned ctx derives from ctx, so ctx's cancellation still stops the
	// attempt.
	StartConsume(ctx context.Context, carrier map[string]string, taskID uint64) (context.Context, func(error))
	// StartSpan starts a child span of ctx; end it with the step's error.
	StartSpan(ctx context.Context, name string) (context.Context, func(error))
}

type nopTracing struct{}

func (nopTracing) StartConsume(ctx context.Context, _ map[string]string, _ uint64) (context.Context, func(error)) {
	return ctx, func(error) {}
}

func (nopTracing) StartSpan(ctx context.Context, _ string) (context.Context, func(error)) {
	return ctx, func(error) {}
}

// WithTracing injects the tracing for the attempt's transfer and fenced
// completion spans. A nil value keeps the no-op default.
func WithTracing(t Tracing) Option {
	return func(s *service) {
		if t != nil {
			s.tracing = t
		}
	}
}
