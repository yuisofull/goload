package auth

import (
	"context"
	stderrors "errors"
	"github.com/yuisofull/goload/internal/errors"
	"time"
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

type VerifyTokenParams struct {
	Token string
}

type VerifyTokenOutput struct {
	AccountID uint64
}

type TokenValidator interface {
	VerifyToken(ctx context.Context, params VerifyTokenParams) (VerifyTokenOutput, error)
}

type Service interface {
	CreateAccount(ctx context.Context, params CreateAccountParams) (CreateAccountOutput, error)
	CreateSession(ctx context.Context, params CreateSessionParams) (CreateSessionOutput, error)
	TokenValidator
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
	exists := s.isAccountNameTaken(ctx, params.AccountName)

	if exists {
		return CreateAccountOutput{}, &errors.Error{Code: errors.ErrCodeAlreadyExists, Message: "account already exists"}
	}

	hash, err := s.passwordHasher.Hash(ctx, params.Password)
	if err != nil {
		return CreateAccountOutput{}, &errors.Error{Code: errors.ErrCodeInternal, Message: "hashing password failed", Cause: err}
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
			return &errors.Error{Code: errors.ErrCodeInternal, Message: "creating account failed", Cause: err}
		}

		accountPassword = &AccountPassword{
			OfAccountId:    accountID,
			HashedPassword: hash,
		}
		if err := s.accountPasswordStore.CreateAccountPassword(ctx, accountPassword); err != nil {
			return &errors.Error{Code: errors.ErrCodeInternal, Message: "storing account password failed", Cause: err}
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
	if (err != nil && stderrors.Is(err, errors.ErrNotFound)) || account == nil {
		return CreateSessionOutput{}, &errors.Error{Code: errors.ErrCodeNotFound, Message: "account not found", Cause: err}
	}

	accountPassword, err := s.accountPasswordStore.GetAccountPassword(ctx, account.Id)
	if err != nil {
		return CreateSessionOutput{}, &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to get password", Cause: err}
	}

	if err := s.passwordHasher.Verify(ctx, params.Password, accountPassword.HashedPassword); err != nil {
		return CreateSessionOutput{}, &errors.Error{Code: ErrCodeInvalidPassword, Message: "invalid password", Cause: err}
	}

	token, err := s.tokenManager.Sign(account.Id)
	if err != nil {
		return CreateSessionOutput{}, err
	}

	return CreateSessionOutput{Token: token, Account: account}, nil
}

func (s *service) isAccountNameTaken(ctx context.Context, accountName string) bool {
	account, err := s.accountStore.GetAccountByAccountName(ctx, accountName)
	if err != nil || account == nil {
		return false
	}

	return true
}

func (s *service) VerifyToken(ctx context.Context, params VerifyTokenParams) (VerifyTokenOutput, error) {
	accountID, err := s.tokenManager.GetAccountIDFrom(params.Token)
	if err != nil {
		return VerifyTokenOutput{}, err
	}
	expiry, err := s.tokenManager.GetExpiryFrom(params.Token)
	if err != nil {
		return VerifyTokenOutput{}, err
	}
	if expiry.Before(time.Now()) {
		return VerifyTokenOutput{}, &errors.Error{Code: ErrCodeInvalidToken, Message: "token expired"}
	}
	return VerifyTokenOutput{AccountID: accountID}, nil
}
