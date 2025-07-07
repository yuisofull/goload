package downloadtasktransport

import (
	"context"
	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/go-kit/log/level"
	"google.golang.org/grpc/metadata"

	"github.com/go-kit/log"
)

func NewLogRequestFunc(logger log.Logger) grpctransport.ClientRequestFunc {
	return func(ctx context.Context, md *metadata.MD) context.Context {
		level.Debug(logger).Log("message", "request received", "method", (*md)["method"])
		return ctx
	}
}

func NewLogResponseFunc(logger log.Logger) grpctransport.ClientResponseFunc {
	return func(ctx context.Context, header metadata.MD, trailer metadata.MD) context.Context {
		level.Debug(logger).Log("message", "response sent")
		return ctx
	}
}
