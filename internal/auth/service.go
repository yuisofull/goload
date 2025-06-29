package auth

import (
	"context"
	"errors"
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

type accountStore interface {
	CreateAccount(ctx context.Context, account *Account) (uint64, error)
	GetAccountByID(ctx context.Context, id uint64) (*Account, error)
	GetAccountByAccountName(ctx context.Context, accountName string) (*Account, error)
}

type accountPasswordStore interface {
	CreateAccountPassword(ctx context.Context, accountPassword *AccountPassword) error
	UpdateAccountPassword(ctx context.Context, accountPassword *AccountPassword) error
}

type service struct {
	accountStore         accountStore
	accountPasswordStore accountPasswordStore
	passwordHasher       PasswordHasher
}

func NewService(accountStore accountStore, accountPasswordStore accountPasswordStore, hasher PasswordHasher) Service {
	return &service{
		accountStore:         accountStore,
		accountPasswordStore: accountPasswordStore,
		passwordHasher:       hasher,
	}
}

func (s *service) CreateAccount(ctx context.Context, params CreateAccountParams) (CreateAccountOutput, error) {
	if exists, err := s.isAccountNameExists(ctx, params.AccountName); err != nil {
		return CreateAccountOutput{}, err
	} else if exists {
		return CreateAccountOutput{}, ErrAccountAlreadyExists
	}

	hash, err := s.passwordHasher.Hash(ctx, params.Password)
	if err != nil {
		return CreateAccountOutput{}, err
	}

	account := &Account{
		AccountName: params.AccountName,
	}

	accountID, err := s.accountStore.CreateAccount(ctx, account)
	if err != nil {
		return CreateAccountOutput{}, err
	}

	accountPassword := &AccountPassword{
		OfAccountId: accountID,
		Hash:        hash,
	}

	if err := s.accountPasswordStore.CreateAccountPassword(ctx, accountPassword); err != nil {
		return CreateAccountOutput{}, err
	}

	return CreateAccountOutput{
		ID:          accountID,
		AccountName: account.AccountName,
	}, nil

}

func (s *service) CreateSession(ctx context.Context, params CreateSessionParams) (CreateSessionOutput, error) {
	return CreateSessionOutput{}, nil
}

func (s *service) isAccountNameExists(ctx context.Context, accountName string) (bool, error) {
	account, err := s.accountStore.GetAccountByAccountName(ctx, accountName)
	if err != nil || account == nil {
		return false, err
	}

	return true, nil
}
