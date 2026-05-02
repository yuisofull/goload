package authendpoint_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/auth"
	authendpoint "github.com/yuisofull/goload/internal/auth/endpoint"
	apperrors "github.com/yuisofull/goload/internal/errors"
)

// ---------------------------------------------------------------------------
// Mock service
// ---------------------------------------------------------------------------

type mockAuthService struct {
	createAccountFn func(ctx context.Context, params auth.CreateAccountParams) (auth.CreateAccountOutput, error)
	createSessionFn func(ctx context.Context, params auth.CreateSessionParams) (auth.CreateSessionOutput, error)
	verifySessionFn func(ctx context.Context, params auth.VerifySessionParams) (auth.VerifySessionOutput, error)
}

func (m *mockAuthService) CreateAccount(
	ctx context.Context,
	params auth.CreateAccountParams,
) (auth.CreateAccountOutput, error) {
	return m.createAccountFn(ctx, params)
}

func (m *mockAuthService) CreateSession(
	ctx context.Context,
	params auth.CreateSessionParams,
) (auth.CreateSessionOutput, error) {
	return m.createSessionFn(ctx, params)
}

func (m *mockAuthService) VerifySession(
	ctx context.Context,
	params auth.VerifySessionParams,
) (auth.VerifySessionOutput, error) {
	return m.verifySessionFn(ctx, params)
}

// ---------------------------------------------------------------------------
// CreateAccount endpoint
// ---------------------------------------------------------------------------

func TestMakeCreateAccountEndpoint_Success(t *testing.T) {
	svc := &mockAuthService{
		createAccountFn: func(_ context.Context, params auth.CreateAccountParams) (auth.CreateAccountOutput, error) {
			assert.Equal(t, "alice", params.AccountName)
			assert.Equal(t, "secret123", params.Password)
			return auth.CreateAccountOutput{ID: 42, AccountName: "alice"}, nil
		},
	}

	ep := authendpoint.MakeCreateAccountEndpoint(svc)
	resp, err := ep(context.Background(), &authendpoint.CreateAccountRequest{
		AccountName: "alice",
		Password:    "secret123",
	})

	require.NoError(t, err)
	out := resp.(*authendpoint.CreateAccountResponse)
	assert.Equal(t, uint64(42), out.AccountId)
}

func TestMakeCreateAccountEndpoint_AlreadyExists(t *testing.T) {
	svc := &mockAuthService{
		createAccountFn: func(_ context.Context, _ auth.CreateAccountParams) (auth.CreateAccountOutput, error) {
			return auth.CreateAccountOutput{}, &apperrors.Error{
				Code:    apperrors.ErrCodeAlreadyExists,
				Message: "account already exists",
			}
		},
	}

	ep := authendpoint.MakeCreateAccountEndpoint(svc)
	_, err := ep(context.Background(), &authendpoint.CreateAccountRequest{
		AccountName: "alice",
		Password:    "secret123",
	})

	require.Error(t, err)
	appErr := apperrors.AsError(err)
	require.NotNil(t, appErr)
	assert.Equal(t, apperrors.ErrCodeAlreadyExists, appErr.Code)
}

func TestMakeCreateAccountEndpoint_EmptyName(t *testing.T) {
	svc := &mockAuthService{
		createAccountFn: func(_ context.Context, _ auth.CreateAccountParams) (auth.CreateAccountOutput, error) {
			return auth.CreateAccountOutput{}, &apperrors.Error{
				Code:    apperrors.ErrCodeInvalidInput,
				Message: "account name is required",
			}
		},
	}

	ep := authendpoint.MakeCreateAccountEndpoint(svc)
	_, err := ep(context.Background(), &authendpoint.CreateAccountRequest{
		AccountName: "",
		Password:    "secret",
	})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

// ---------------------------------------------------------------------------
// CreateSession endpoint
// ---------------------------------------------------------------------------

func TestMakeCreateSessionEndpoint_Success(t *testing.T) {
	svc := &mockAuthService{
		createSessionFn: func(_ context.Context, params auth.CreateSessionParams) (auth.CreateSessionOutput, error) {
			assert.Equal(t, "alice", params.AccountName)
			assert.Equal(t, "secret123", params.Password)
			return auth.CreateSessionOutput{
				Token:   "jwt-token",
				Account: &auth.Account{Id: 1, AccountName: "alice"},
			}, nil
		},
	}

	ep := authendpoint.MakeCreateSessionEndpoint(svc)
	resp, err := ep(context.Background(), &authendpoint.CreateSessionRequest{
		AccountName: "alice",
		Password:    "secret123",
	})

	require.NoError(t, err)
	out := resp.(*authendpoint.CreateSessionResponse)
	assert.Equal(t, "jwt-token", out.Token)
	require.NotNil(t, out.Account)
	assert.Equal(t, uint64(1), out.Account.GetId())
	assert.Equal(t, "alice", out.Account.GetAccountName())
}

func TestMakeCreateSessionEndpoint_AccountNotFound(t *testing.T) {
	svc := &mockAuthService{
		createSessionFn: func(_ context.Context, _ auth.CreateSessionParams) (auth.CreateSessionOutput, error) {
			return auth.CreateSessionOutput{}, &apperrors.Error{
				Code:    apperrors.ErrCodeNotFound,
				Message: "account not found",
			}
		},
	}

	ep := authendpoint.MakeCreateSessionEndpoint(svc)
	_, err := ep(context.Background(), &authendpoint.CreateSessionRequest{
		AccountName: "ghost",
		Password:    "pass",
	})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeNotFound))
}

func TestMakeCreateSessionEndpoint_WrongPassword(t *testing.T) {
	svc := &mockAuthService{
		createSessionFn: func(_ context.Context, _ auth.CreateSessionParams) (auth.CreateSessionOutput, error) {
			return auth.CreateSessionOutput{}, &apperrors.Error{
				Code:    auth.ErrCodeInvalidPassword,
				Message: "invalid password",
			}
		},
	}

	ep := authendpoint.MakeCreateSessionEndpoint(svc)
	_, err := ep(context.Background(), &authendpoint.CreateSessionRequest{
		AccountName: "alice",
		Password:    "wrong",
	})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, auth.ErrCodeInvalidPassword))
}

// ---------------------------------------------------------------------------
// VerifyToken endpoint
// ---------------------------------------------------------------------------

func TestMakeVerifyTokenEndpoint_Success(t *testing.T) {
	svc := &mockAuthService{
		verifySessionFn: func(_ context.Context, params auth.VerifySessionParams) (auth.VerifySessionOutput, error) {
			assert.Equal(t, "valid-jwt", params.Token)
			return auth.VerifySessionOutput{AccountID: 7}, nil
		},
	}

	ep := authendpoint.MakeVerifyTokenEndpoint(svc)
	resp, err := ep(context.Background(), &authendpoint.VerifyTokenRequest{Token: "valid-jwt"})

	require.NoError(t, err)
	out := resp.(*authendpoint.VerifyTokenResponse)
	assert.Equal(t, uint64(7), out.AccountId)
}

func TestMakeVerifyTokenEndpoint_InvalidToken(t *testing.T) {
	svc := &mockAuthService{
		verifySessionFn: func(_ context.Context, _ auth.VerifySessionParams) (auth.VerifySessionOutput, error) {
			return auth.VerifySessionOutput{}, &apperrors.Error{
				Code:    auth.ErrCodeInvalidToken,
				Message: "token expired",
			}
		},
	}

	ep := authendpoint.MakeVerifyTokenEndpoint(svc)
	_, err := ep(context.Background(), &authendpoint.VerifyTokenRequest{Token: "expired-token"})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, auth.ErrCodeInvalidToken))
}

// ---------------------------------------------------------------------------
// Set — full endpoint set with rate limiter
// ---------------------------------------------------------------------------

func TestSet_CreateAccount_RoundTrip(t *testing.T) {
	svc := &mockAuthService{
		createAccountFn: func(_ context.Context, params auth.CreateAccountParams) (auth.CreateAccountOutput, error) {
			return auth.CreateAccountOutput{ID: 99, AccountName: params.AccountName}, nil
		},
	}

	set := authendpoint.New(svc)
	out, err := set.CreateAccount(context.Background(), auth.CreateAccountParams{
		AccountName: "bob",
		Password:    "pass",
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(99), out.ID)
	assert.Equal(t, "bob", out.AccountName)
}

func TestSet_CreateSession_RoundTrip(t *testing.T) {
	svc := &mockAuthService{
		createSessionFn: func(_ context.Context, params auth.CreateSessionParams) (auth.CreateSessionOutput, error) {
			return auth.CreateSessionOutput{
				Token:   "tok",
				Account: &auth.Account{Id: 5, AccountName: params.AccountName},
			}, nil
		},
	}

	set := authendpoint.New(svc)
	out, err := set.CreateSession(context.Background(), auth.CreateSessionParams{
		AccountName: "bob",
		Password:    "pass",
	})

	require.NoError(t, err)
	assert.Equal(t, "tok", out.Token)
	assert.Equal(t, uint64(5), out.Account.Id)
}

func TestSet_VerifySession_RoundTrip(t *testing.T) {
	svc := &mockAuthService{
		verifySessionFn: func(_ context.Context, params auth.VerifySessionParams) (auth.VerifySessionOutput, error) {
			return auth.VerifySessionOutput{AccountID: 3}, nil
		},
	}

	set := authendpoint.New(svc)
	out, err := set.VerifySession(context.Background(), auth.VerifySessionParams{Token: "t"})

	require.NoError(t, err)
	assert.Equal(t, uint64(3), out.AccountID)
}
