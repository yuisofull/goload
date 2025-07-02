package auth

import (
	"context"
	"errors"
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
	Token string
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
	if exists, err := s.isAccountNameTaken(ctx, params.AccountName); err != nil {
		return CreateAccountOutput{}, err
	} else if exists {
		return CreateAccountOutput{}, ErrAccountAlreadyExists
	}

	hash, err := s.passwordHasher.Hash(ctx, params.Password)
	if err != nil {
		return CreateAccountOutput{}, err
	}

	var (
		accountID       uint64
		account         *Account
		accountPassword *AccountPassword
	)

	if err := s.txManager.DoInTx(ctx, func(ctx context.Context) error {
		account = &Account{
			AccountName: params.AccountName,
		}

		accountID, err := s.accountStore.CreateAccount(ctx, account)
		if err != nil {
			return err
		}

		accountPassword = &AccountPassword{
			OfAccountId:    accountID,
			HashedPassword: hash,
		}
		return s.accountPasswordStore.CreateAccountPassword(ctx, accountPassword)
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
	if err != nil {
		return CreateSessionOutput{}, err
	}

	accountPassword, err := s.accountPasswordStore.GetAccountPassword(ctx, account.Id)
	if err != nil {
		return CreateSessionOutput{}, err
	}

	if err := s.passwordHasher.Verify(ctx, params.Password, accountPassword.HashedPassword); err != nil {
		return CreateSessionOutput{}, err
	}

	token, err := s.tokenManager.Sign(account.Id)
	if err != nil {
		return CreateSessionOutput{}, err
	}

	return CreateSessionOutput{
		Token: token,
	}, nil
}

func (s *service) isAccountNameTaken(ctx context.Context, accountName string) (bool, error) {
	account, err := s.accountStore.GetAccountByAccountName(ctx, accountName)
	if err != nil || account == nil {
		return false, err
	}

	return true, nil
}
