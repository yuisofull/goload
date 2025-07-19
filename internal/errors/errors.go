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

func EncodeGRPCError(err error) error {
	var svcErr *Error

	if errors.As(err, &svcErr) {
		msg := svcErr.Message
		if svcErr.Cause != nil {
			msg += ": " + svcErr.Cause.Error()
		}
		switch svcErr.Code {
		case ErrCodeAlreadyExists:
			return status.Error(codes.AlreadyExists, msg)
		case ErrCodeNotFound:
			return status.Error(codes.NotFound, msg)
		case ErrCodeInternal:
			return status.Error(codes.Internal, msg)
		case ErrCodeUnauthenticated:
			return status.Error(codes.Unauthenticated, msg)
		case ErrCodePermissionDenied:
			return status.Error(codes.PermissionDenied, msg)
		default:
			return status.Error(codes.Unknown, msg)
		}
	}

	return status.Error(codes.Unknown, err.Error())
}
