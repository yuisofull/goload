package auth

import "fmt"

type ErrorCode string

const (
	ErrCodeAlreadyExists   ErrorCode = "ALREADY_EXISTS"
	ErrCodeNotFound        ErrorCode = "NOT_FOUND"
	ErrCodeInvalidPassword ErrorCode = "INVALID_PASSWORD"
	ErrCodeInternal        ErrorCode = "INTERNAL"
)

type ServiceError struct {
	Code    ErrorCode
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

func NewServiceError(code ErrorCode, msg string, cause error) *ServiceError {
	return &ServiceError{
		Code:    code,
		Message: msg,
		Cause:   cause,
	}
}
