package rabbitmq

import "context"

type contextKey int

const (
	_ contextKey = iota
	redeliveredContextKey
)

func setRedeliveredToCtx(ctx context.Context, redelivered bool) context.Context {
	return context.WithValue(ctx, redeliveredContextKey, redelivered)
}

func MessageRedeliveredFromCtx(ctx context.Context) (bool, bool) {
	redelivered, ok := ctx.Value(redeliveredContextKey).(bool)
	return redelivered, ok
}
