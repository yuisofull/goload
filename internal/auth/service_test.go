package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/auth"
	apperrors "github.com/yuisofull/goload/internal/errors"
)

type stubAccountStore struct {
	createFn    func(ctx context.Context, account *auth.Account) (uint64, error)
	getByNameFn func(ctx context.Context, accountName string) (*auth.Account, error)
}

func (s *stubAccountStore) CreateAccount(ctx context.Context, account *auth.Account) (uint64, error) {
	return s.createFn(ctx, account)
}

func (s *stubAccountStore) GetAccountByID(_ context.Context, _ uint64) (*auth.Account, error) {
	return nil, apperrors.ErrNotFound
}

func (s *stubAccountStore) GetAccountByAccountName(ctx context.Context, accountName string) (*auth.Account, error) {
	return s.getByNameFn(ctx, accountName)
}

type stubPasswordStore struct {
	createFn func(ctx context.Context, accountPassword *auth.AccountPassword) error
	getFn    func(ctx context.Context, ofAccountID uint64) (auth.AccountPassword, error)
}

func (s *stubPasswordStore) CreateAccountPassword(ctx context.Context, accountPassword *auth.AccountPassword) error {
	if s.createFn != nil {
		return s.createFn(ctx, accountPassword)
	}
	return nil
}

func (s *stubPasswordStore) UpdateAccountPassword(_ context.Context, _ *auth.AccountPassword) error {
	return nil
}

func (s *stubPasswordStore) GetAccountPassword(ctx context.Context, ofAccountID uint64) (auth.AccountPassword, error) {
	if s.getFn != nil {
		return s.getFn(ctx, ofAccountID)
	}
	return auth.AccountPassword{}, apperrors.ErrNotFound
}

type stubTxManager struct{}

func (stubTxManager) DoInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type stubHasher struct {
	verifyErr error
}

func (s stubHasher) Hash(_ context.Context, password string) (string, error) {
	return "hashed:" + password, nil
}

func (s stubHasher) Verify(_ context.Context, _, _ string) error {
	return s.verifyErr
}

type stubTokenManager struct{}

func (stubTokenManager) Sign(_ uint64) (string, error) { return "token", nil }

func (stubTokenManager) GetAccountIDFrom(_ string) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (stubTokenManager) GetExpiryFrom(_ string) (time.Time, error) {
	return time.Time{}, errors.New("not implemented")
}

func TestCreateAccount_DuplicateName(t *testing.T) {
	t.Parallel()

	store := &stubAccountStore{
		createFn: func(_ context.Context, _ *auth.Account) (uint64, error) {
			return 0, &apperrors.Error{
				Code:    apperrors.ErrCodeAlreadyExists,
				Message: "account already exists",
			}
		},
	}
	svc := auth.NewService(store, &stubPasswordStore{}, stubTxManager{}, stubHasher{}, stubTokenManager{})

	_, err := svc.CreateAccount(context.Background(), auth.CreateAccountParams{
		AccountName: "alice",
		Password:    "secret",
	})
	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeAlreadyExists))
}

func TestCreateSession_StoreFailureIsInternal(t *testing.T) {
	t.Parallel()

	store := &stubAccountStore{
		getByNameFn: func(_ context.Context, _ string) (*auth.Account, error) {
			return nil, errors.New("mysql: connection refused")
		},
	}
	svc := auth.NewService(store, &stubPasswordStore{}, stubTxManager{}, stubHasher{}, stubTokenManager{})

	_, err := svc.CreateSession(context.Background(), auth.CreateSessionParams{
		AccountName: "alice",
		Password:    "secret",
	})
	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInternal))
	assert.False(t, apperrors.IsError(err, apperrors.ErrCodeNotFound))
}

func TestCreateSession_NotFound(t *testing.T) {
	t.Parallel()

	store := &stubAccountStore{
		getByNameFn: func(_ context.Context, _ string) (*auth.Account, error) {
			return nil, apperrors.ErrNotFound
		},
	}
	svc := auth.NewService(store, &stubPasswordStore{}, stubTxManager{}, stubHasher{}, stubTokenManager{})

	_, err := svc.CreateSession(context.Background(), auth.CreateSessionParams{
		AccountName: "ghost",
		Password:    "secret",
	})
	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeNotFound))
}
