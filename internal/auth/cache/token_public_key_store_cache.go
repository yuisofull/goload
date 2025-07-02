package authcache

import (
	"context"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/pkg/cache"
)

type tokenPublicKeyStoreCache struct {
	logger log.Logger
	cache  cache.Cache[TokenPublicKeyCacheKey, []byte]
	next   auth.TokenPublicKeyStore
}

type TokenPublicKeyCacheKey struct {
	kid uint64
}

func NewTokenPublicKeyStore(logger log.Logger, cache cache.Cache[TokenPublicKeyCacheKey, []byte], next auth.TokenPublicKeyStore) auth.TokenPublicKeyStore {
	return &tokenPublicKeyStoreCache{
		cache: cache,
		next:  next,
	}
}

func (t *tokenPublicKeyStoreCache) CreateTokenPublicKey(ctx context.Context, tokenPublicKey *auth.TokenPublicKey) (kid uint64, err error) {
	t.cache.Set(ctx, TokenPublicKeyCacheKey{kid: kid}, tokenPublicKey.PublicKey, 0)
	return t.next.CreateTokenPublicKey(ctx, tokenPublicKey)
}

func (t *tokenPublicKeyStoreCache) GetTokenPublicKey(ctx context.Context, kid uint64) (auth.TokenPublicKey, error) {
	if v, ok := t.cache.Get(ctx, TokenPublicKeyCacheKey{kid: kid}); ok {
		return auth.TokenPublicKey{Id: kid, PublicKey: v}, nil
	}
	level.Info(t.logger).Log("msg", "token public key not found in cache, fetching from db", "kid", kid)

	tokenPublicKey, err := t.next.GetTokenPublicKey(ctx, kid)
	if err != nil {
		return auth.TokenPublicKey{}, err
	}

	t.cache.Set(ctx, TokenPublicKeyCacheKey{kid: kid}, tokenPublicKey.PublicKey, 0)
	return tokenPublicKey, nil
}
