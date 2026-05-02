package authtransport

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	internalerrors "github.com/yuisofull/goload/internal/errors"
)

func TestEncodeError_NotFoundMapsToGRPCNotFound(t *testing.T) {
	err := encodeError(
		context.Background(),
		&internalerrors.Error{Code: internalerrors.ErrCodeNotFound, Message: "account not found"},
	)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}
