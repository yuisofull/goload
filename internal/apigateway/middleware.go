package apigateway

import (
	"context"
	"github.com/yuisofull/goload/internal/auth"

	"github.com/go-kit/kit/endpoint"
	"github.com/yuisofull/goload/internal/errors"
)

// Custom context key type for type safety
type contextKey int

// Context keys
const (
	tokenKey contextKey = iota
	accountIDKey
)

// AuthMiddleware extracts and validates JWT tokens from HTTP requests
type AuthMiddleware struct {
	tokenValidator auth.TokenValidator
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(tokenValidator auth.TokenValidator) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (interface{}, error) {
			// Extract token from context (set by HTTP middleware)
			token, ok := ctx.Value(tokenKey).(string)
			if !ok || token == "" {
				return nil, &errors.Error{Code: errors.ErrCodeUnauthenticated, Message: "missing or invalid token"}
			}

			out, err := tokenValidator.VerifyToken(ctx, auth.VerifyTokenParams{Token: token})
			if err != nil {
				return nil, err
			}

			// Add user ID to context
			ctx = context.WithValue(ctx, accountIDKey, out.AccountID)

			// Call the next endpoint
			return next(ctx, request)
		}
	}
}

// UserIDFromContext extracts user ID from context
func UserIDFromContext(ctx context.Context) (uint64, bool) {
	userID, ok := ctx.Value(accountIDKey).(uint64)
	return userID, ok
}
