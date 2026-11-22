package download

import "time"

// Metrics records download-worker metrics. The service calls it on the transfer
// path; the OTel-backed implementation is wired in cmd. nopMetrics is the
// default, so core needs no metrics backend (CC-2).
type Metrics interface {
	// RecordTransfer records one attempt's byte-transfer duration (streaming
	// only, excluding the metadata probe and the storing/completion step).
	RecordTransfer(d time.Duration)
	// IncRedelivered counts a dispatch message the broker redelivered.
	IncRedelivered()
}

type nopMetrics struct{}

func (nopMetrics) RecordTransfer(time.Duration) {}
func (nopMetrics) IncRedelivered()              {}

// WithMetrics injects a metrics recorder. A nil recorder keeps the no-op default.
func WithMetrics(m Metrics) Option {
	return func(s *service) {
		if m != nil {
			s.metrics = m
		}
	}
}
