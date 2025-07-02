package authcache

import (
	"context"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/pkg/cache"
)

type accountStoreCache struct {
	nameCache cache.SetCache[AccountNameTakenSetKey, string]
	next      auth.AccountStore
}

type AccountNameTakenSetKey struct{}

func NewAccountStore(nameCache cache.SetCache[AccountNameTakenSetKey, string], next auth.AccountStore) auth.AccountStore {
	return &accountStoreCache{
		nameCache: nameCache,
		next:      next,
	}
}

func (a *accountStoreCache) CreateAccount(ctx context.Context, account *auth.Account) (uint64, error) {
	if contain, err := a.isAccountNameTaken(ctx, account.AccountName); err != nil {
		return 0, err
	} else if contain {
		return 0, auth.ErrAccountAlreadyExists
	}

	accountID, err := a.next.CreateAccount(ctx, account)
	if err != nil {
		return 0, err
	}

	if err := a.nameCache.Add(ctx, AccountNameTakenSetKey{}, account.AccountName); err != nil {
		return 0, err
	}

	return accountID, nil
}

func (a *accountStoreCache) GetAccountByID(ctx context.Context, id uint64) (*auth.Account, error) {
	return a.next.GetAccountByID(ctx, id)
}

func (a *accountStoreCache) GetAccountByAccountName(ctx context.Context, accountName string) (*auth.Account, error) {
	return a.next.GetAccountByAccountName(ctx, accountName)
}

func (a *accountStoreCache) isAccountNameTaken(ctx context.Context, accountName string) (contain bool, err error) {
	return a.nameCache.Contains(ctx, AccountNameTakenSetKey{}, accountName)
}
