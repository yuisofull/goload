package apigateway

import (
	"context"

	"github.com/go-kit/kit/endpoint"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/task"
)

// RequireTaskOwnerMiddleware returns an endpoint middleware that verifies the
// authenticated user (from context) is the owner of the task identified by
// idFn(request). It returns Unauthenticated if user not in context, NotFound
// if svc.GetTask returns not found, or PermissionDenied if owner mismatch.
func RequireTaskOwnerMiddleware(svc task.Service, idFn func(req interface{}) uint64) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (interface{}, error) {
			userID, ok := UserIDFromContext(ctx)
			if !ok {
				return nil, &errors.Error{Code: errors.ErrCodeUnauthenticated, Message: "unauthenticated"}
			}

			id := idFn(request)
			t, err := svc.GetTask(ctx, id)
			if err != nil {
				return nil, err
			}
			if t.OfAccountID != userID {
				return nil, &errors.Error{Code: errors.ErrCodePermissionDenied, Message: "permission denied"}
			}

			return next(ctx, request)
		}
	}
}
