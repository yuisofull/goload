package apigateway

import (
	"context"
	"github.com/yuisofull/goload/internal/auth"

	"github.com/go-kit/kit/endpoint"
	"github.com/yuisofull/goload/internal/errors"
)

type contextKey int

const (
	tokenKey contextKey = iota
	accountIDKey
)

type AuthMiddleware struct {
	tokenValidator auth.SessionValidator
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(tokenValidator auth.SessionValidator) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (interface{}, error) {
			token, ok := ctx.Value(tokenKey).(string)
			if !ok || token == "" {
				return nil, &errors.Error{Code: errors.ErrCodeUnauthenticated, Message: "missing or invalid token"}
			}

			out, err := tokenValidator.VerifySession(ctx, auth.VerifySessionParams{Token: token})
			if err != nil {
				return nil, err
			}

			ctx = context.WithValue(ctx, accountIDKey, out.AccountID)

			return next(ctx, request)
		}
	}
}

func UserIDFromContext(ctx context.Context) (uint64, bool) {
	userID, ok := ctx.Value(accountIDKey).(uint64)
	return userID, ok
}
