package errors_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	internalerrors "github.com/yuisofull/goload/internal/errors"
)

func TestEncodeGRPCError_MapsDomainCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code internalerrors.Code
		want codes.Code
	}{
		{name: "already exists", code: internalerrors.ErrCodeAlreadyExists, want: codes.AlreadyExists},
		{name: "not found", code: internalerrors.ErrCodeNotFound, want: codes.NotFound},
		{name: "internal", code: internalerrors.ErrCodeInternal, want: codes.Internal},
		{name: "unauthenticated", code: internalerrors.ErrCodeUnauthenticated, want: codes.Unauthenticated},
		{name: "permission denied", code: internalerrors.ErrCodePermissionDenied, want: codes.PermissionDenied},
		{name: "invalid input", code: internalerrors.ErrCodeInvalidInput, want: codes.InvalidArgument},
		{name: "conflict", code: internalerrors.ErrCodeConflict, want: codes.Aborted},
		{name: "invalid state", code: internalerrors.ErrCodeInvalidState, want: codes.FailedPrecondition},
		{name: "too many requests", code: internalerrors.ErrCodeTooManyRequests, want: codes.ResourceExhausted},
		{name: "unknown", code: internalerrors.ErrCodeUnknown, want: codes.Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := internalerrors.EncodeGRPCError(&internalerrors.Error{
				Code:    tt.code,
				Message: "safe client message",
				Cause:   errors.New("pq: secret table users leaked"),
			})
			st, ok := status.FromError(err)
			require.True(t, ok)
			require.Equal(t, tt.want, st.Code())
			require.Equal(t, "safe client message", st.Message())
			require.NotContains(t, st.Message(), "pq:")
			require.NotContains(t, st.Message(), "users")
		})
	}
}

func TestEncodeGRPCError_UnclassifiedErrorDoesNotLeak(t *testing.T) {
	t.Parallel()

	err := internalerrors.EncodeGRPCError(errors.New("sql: connection refused"))
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unknown, st.Code())
	require.Equal(t, "internal server error", st.Message())
}
