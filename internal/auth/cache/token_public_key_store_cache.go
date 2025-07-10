package authcache

import (
	"context"
	stdErrors "errors"
	"fmt"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/pkg/cache"
)

type CacheErrorHandler func(context.Context, error)

type tokenPublicKeyStoreCache struct {
	cacheErrorHandler CacheErrorHandler
	cache             cache.Cache[TokenPublicKeyCacheKey, []byte]
	next              auth.TokenPublicKeyStore
}

type TokenPublicKeyCacheKey struct {
	kid uint64
}

func NewTokenPublicKeyStore(cache cache.Cache[TokenPublicKeyCacheKey, []byte], next auth.TokenPublicKeyStore, cacheErrorHandler CacheErrorHandler) auth.TokenPublicKeyStore {
	return &tokenPublicKeyStoreCache{
		cache:             cache,
		next:              next,
		cacheErrorHandler: cacheErrorHandler,
	}
}

func (t *tokenPublicKeyStoreCache) CreateTokenPublicKey(ctx context.Context, tokenPublicKey *auth.TokenPublicKey) (kid uint64, err error) {
	if err := t.cache.Set(ctx, TokenPublicKeyCacheKey{kid: kid}, tokenPublicKey.PublicKey, 0); err != nil {
		t.cacheErrorHandler(ctx, fmt.Errorf("failed to set token public key to cache: %w", err))
	}
	return t.next.CreateTokenPublicKey(ctx, tokenPublicKey)
}

func (t *tokenPublicKeyStoreCache) GetTokenPublicKey(ctx context.Context, kid uint64) (auth.TokenPublicKey, error) {
	v, err := t.cache.Get(ctx, TokenPublicKeyCacheKey{kid: kid})
	if err == nil {
		return auth.TokenPublicKey{Id: kid, PublicKey: v}, nil
	}

	if !stdErrors.Is(err, cache.Nil) {
		t.cacheErrorHandler(ctx, fmt.Errorf("failed to get token public key from cache: %w", err))
	} else {
		t.cacheErrorHandler(ctx, fmt.Errorf("failed to get token public key from cache: cache miss"))
	}

	tokenPublicKey, err := t.next.GetTokenPublicKey(ctx, kid)
	if err != nil {
		return auth.TokenPublicKey{}, err
	}

	if err := t.cache.Set(ctx, TokenPublicKeyCacheKey{kid: kid}, tokenPublicKey.PublicKey, 0); err != nil {
		t.cacheErrorHandler(ctx, fmt.Errorf("failed to set token public key to cache: %w", err))
	}
	return tokenPublicKey, nil
}
