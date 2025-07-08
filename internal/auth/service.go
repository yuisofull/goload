package auth

import (
	"context"
	"errors"
	error2 "github.com/yuisofull/goload/internal/errors"
	"time"
)

var (
	ErrAccountAlreadyExists = errors.New("account already exists")
)

type CreateAccountParams struct {
	AccountName string
	Password    string
}

type CreateAccountOutput struct {
	ID          uint64
	AccountName string
}

type CreateSessionParams struct {
	AccountName string
	Password    string
}

type CreateSessionOutput struct {
	Token   string
	Account *Account
}

type Service interface {
	CreateAccount(ctx context.Context, params CreateAccountParams) (CreateAccountOutput, error)
	CreateSession(ctx context.Context, params CreateSessionParams) (CreateSessionOutput, error)
}

type AccountStore interface {
	CreateAccount(ctx context.Context, account *Account) (uint64, error)
	GetAccountByID(ctx context.Context, id uint64) (*Account, error)
	GetAccountByAccountName(ctx context.Context, accountName string) (*Account, error)
}

type AccountPasswordStore interface {
	CreateAccountPassword(ctx context.Context, accountPassword *AccountPassword) error
	UpdateAccountPassword(ctx context.Context, accountPassword *AccountPassword) error
	GetAccountPassword(ctx context.Context, ofAccountID uint64) (AccountPassword, error)
}

type TxManager interface {
	DoInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	Verify(ctx context.Context, password, hashedPassword string) error
}

type TokenManager interface {
	Sign(accountID uint64) (string, error)
	GetAccountIDFrom(token string) (uint64, error)
	GetExpiryFrom(token string) (time.Time, error)
}

type service struct {
	accountStore         AccountStore
	accountPasswordStore AccountPasswordStore
	passwordHasher       PasswordHasher
	txManager            TxManager
	tokenManager         TokenManager
}

func NewService(
	accountStore AccountStore,
	accountPasswordStore AccountPasswordStore,
	txManager TxManager,
	hasher PasswordHasher,
	tokenManager TokenManager,
) Service {
	return &service{
		accountStore:         accountStore,
		accountPasswordStore: accountPasswordStore,
		passwordHasher:       hasher,
		txManager:            txManager,
		tokenManager:         tokenManager,
	}
}

func (s *service) CreateAccount(ctx context.Context, params CreateAccountParams) (CreateAccountOutput, error) {
	exists, err := s.isAccountNameTaken(ctx, params.AccountName)
	if err != nil {
		return CreateAccountOutput{}, error2.NewServiceError(error2.ErrCodeInternal, "checking account name failed", err)
	}
	if exists {
		return CreateAccountOutput{}, error2.NewServiceError(error2.ErrCodeAlreadyExists, "account already exists", nil)
	}

	hash, err := s.passwordHasher.Hash(ctx, params.Password)
	if err != nil {
		return CreateAccountOutput{}, error2.NewServiceError(error2.ErrCodeInternal, "hashing password failed", err)
	}

	var (
		accountID       uint64
		account         *Account
		accountPassword *AccountPassword
	)

	if err := s.txManager.DoInTx(ctx, func(ctx context.Context) error {
		account = &Account{AccountName: params.AccountName}

		accountID, err = s.accountStore.CreateAccount(ctx, account)
		if err != nil {
			return error2.NewServiceError(error2.ErrCodeInternal, "creating account failed", err)
		}

		accountPassword = &AccountPassword{
			OfAccountId:    accountID,
			HashedPassword: hash,
		}
		if err := s.accountPasswordStore.CreateAccountPassword(ctx, accountPassword); err != nil {
			return error2.NewServiceError(error2.ErrCodeInternal, "storing account password failed", err)
		}
		return nil
	}); err != nil {
		return CreateAccountOutput{}, err
	}

	return CreateAccountOutput{
		ID:          accountID,
		AccountName: account.AccountName,
	}, nil
}

func (s *service) CreateSession(ctx context.Context, params CreateSessionParams) (CreateSessionOutput, error) {
	account, err := s.accountStore.GetAccountByAccountName(ctx, params.AccountName)
	if err != nil || account == nil {
		return CreateSessionOutput{}, error2.NewServiceError(error2.ErrCodeNotFound, "account not found", err)
	}

	accountPassword, err := s.accountPasswordStore.GetAccountPassword(ctx, account.Id)
	if err != nil {
		return CreateSessionOutput{}, error2.NewServiceError(error2.ErrCodeInternal, "failed to get password", err)
	}

	if err := s.passwordHasher.Verify(ctx, params.Password, accountPassword.HashedPassword); err != nil {
		return CreateSessionOutput{}, error2.NewServiceError(ErrCodeInvalidPassword, "invalid password", err)
	}

	token, err := s.tokenManager.Sign(account.Id)
	if err != nil {
		return CreateSessionOutput{}, error2.NewServiceError(error2.ErrCodeInternal, "token signing failed", err)
	}

	return CreateSessionOutput{Token: token, Account: account}, nil
}

func (s *service) isAccountNameTaken(ctx context.Context, accountName string) (bool, error) {
	account, err := s.accountStore.GetAccountByAccountName(ctx, accountName)
	if err != nil || account == nil {
		return false, err
	}

	return true, nil
}
