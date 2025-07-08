package errors

import (
	"errors"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Code string

const (
	ErrCodeAlreadyExists    Code = "ALREADY_EXISTS"
	ErrCodeNotFound         Code = "NOT_FOUND"
	ErrCodeInternal         Code = "INTERNAL"
	ErrCodeUnknown          Code = "UNKNOWN"
	ErrCodeUnauthenticated  Code = "UNAUTHENTICATED"
	ErrCodePermissionDenied Code = "PERMISSION_DENIED"
)

type ServiceError struct {
	Code    Code
	Message string
	Cause   error
}

func (e *ServiceError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ServiceError) Unwrap() error {
	return e.Cause
}

func NewServiceError(code Code, msg string, cause error) *ServiceError {
	return &ServiceError{
		Code:    code,
		Message: msg,
		Cause:   cause,
	}
}

func EncodeGRPCError(err error) error {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case ErrCodeAlreadyExists:
			return status.Error(codes.AlreadyExists, svcErr.Message)
		case ErrCodeNotFound:
			return status.Error(codes.NotFound, svcErr.Message)
		case ErrCodeInternal:
			return status.Error(codes.Internal, svcErr.Message)
		case ErrCodeUnauthenticated:
			return status.Error(codes.Unauthenticated, svcErr.Message)
		case ErrCodePermissionDenied:
			return status.Error(codes.PermissionDenied, svcErr.Message)
		default:
			return status.Error(codes.Unknown, svcErr.Message)
		}
	}

	return status.Error(codes.Unknown, err.Error())
}
