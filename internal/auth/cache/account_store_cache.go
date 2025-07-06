package authcache

import (
	"context"
	"errors"
	"fmt"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/pkg/cache"
)

var (
	ErrCacheMiss = errors.New("cache miss")
)

type accountStoreCache struct {
	cacheErrorHandler CacheErrorHandler
	nameCache         cache.SetCache[AccountNameTakenSetKey, string]
	next              auth.AccountStore
}

type AccountNameTakenSetKey struct{}

func NewAccountStore(nameCache cache.SetCache[AccountNameTakenSetKey, string], next auth.AccountStore, cacheErrorHandler CacheErrorHandler) auth.AccountStore {
	return &accountStoreCache{
		nameCache: nameCache,
		next:      next,
	}
}

func (a *accountStoreCache) CreateAccount(ctx context.Context, account *auth.Account) (uint64, error) {
	var contain bool
	var err error
	if contain, err = a.isAccountNameTaken(ctx, account.AccountName); err == nil && contain {
		return 0, auth.ErrAccountAlreadyExists
	}

	a.cacheErrorHandler(ctx, err)

	accountID, err := a.next.CreateAccount(ctx, account)
	if err != nil {
		return 0, err
	}

	if err := a.nameCache.Add(ctx, AccountNameTakenSetKey{}, account.AccountName); err != nil {
		a.cacheErrorHandler(ctx, fmt.Errorf("failed to add account name to cache: %w", err))
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
