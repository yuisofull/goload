package rabbitmq

import (
	"maps"
	"slices"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type logFields map[string]any

// loggerAdapter gives go-kit the shape of watermill.LoggerAdapter the adapted code logs through.
type loggerAdapter struct {
	logger log.Logger
}

func (l loggerAdapter) Error(msg string, err error, fields logFields) {
	log.With(level.Error(l.logger), "msg", msg, "err", err).Log(fields.keyvals()...)
}

func (l loggerAdapter) Info(msg string, fields logFields) {
	log.With(level.Info(l.logger), "msg", msg).Log(fields.keyvals()...)
}

func (l loggerAdapter) Debug(msg string, fields logFields) {
	log.With(level.Debug(l.logger), "msg", msg).Log(fields.keyvals()...)
}

// keyvals orders the fields: map iteration would shuffle every log line.
func (f logFields) keyvals() []any {
	var keyvals []any
	for _, key := range slices.Sorted(maps.Keys(f)) {
		keyvals = append(keyvals, key, f[key])
	}
	return keyvals
}
