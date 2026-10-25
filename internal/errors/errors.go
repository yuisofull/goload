package errors

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Code string

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInternal     = errors.New("internal error")
	ErrInvalidInput = errors.New("invalid input")
)

const (
	ErrCodeAlreadyExists    Code = "ALREADY_EXISTS"
	ErrCodeNotFound         Code = "NOT_FOUND"
	ErrCodeInternal         Code = "INTERNAL"
	ErrCodeUnknown          Code = "UNKNOWN"
	ErrCodeUnauthenticated  Code = "UNAUTHENTICATED"
	ErrCodePermissionDenied Code = "PERMISSION_DENIED"
	ErrCodeInvalidInput     Code = "INVALID_INPUT"
	ErrCodeTooManyRequests  Code = "TOO_MANY_REQUESTS"
	ErrCodeInvalidState     Code = "INVALID_STATE"
	ErrCodeConflict         Code = "CONFLICT"
)

type Error struct {
	Code    Code
	Message string
	Cause   error
}

func AsError(err error) *Error {
	var svcErr *Error
	if errors.As(err, &svcErr) {
		return svcErr
	}
	return nil
}

func IsError(err error, code Code) bool {
	svcErr := AsError(err)
	if svcErr == nil {
		return false
	}
	return svcErr.Code == code
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// EncodeGRPCError maps a domain error to a gRPC status.
// The client-facing message is Error.Message only; Cause is for server-side logs.
func EncodeGRPCError(err error) error {
	var svcErr *Error

	if errors.As(err, &svcErr) {
		return status.Error(GRPCCode(svcErr.Code), svcErr.Message)
	}

	return status.Error(codes.Unknown, "internal server error")
}

// GRPCCode maps a domain error code to a gRPC status code.
func GRPCCode(code Code) codes.Code {
	switch code {
	case ErrCodeAlreadyExists:
		return codes.AlreadyExists
	case ErrCodeNotFound:
		return codes.NotFound
	case ErrCodeInternal:
		return codes.Internal
	case ErrCodeUnauthenticated:
		return codes.Unauthenticated
	case ErrCodePermissionDenied:
		return codes.PermissionDenied
	case ErrCodeInvalidInput:
		return codes.InvalidArgument
	case ErrCodeConflict:
		return codes.Aborted
	case ErrCodeInvalidState:
		return codes.FailedPrecondition
	case ErrCodeTooManyRequests:
		return codes.ResourceExhausted
	case ErrCodeUnknown:
		return codes.Unknown
	default:
		return codes.Unknown
	}
}
