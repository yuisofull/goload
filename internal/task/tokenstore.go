package task

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/pkg/cache"
	rediscache "github.com/yuisofull/goload/pkg/cache/redis"
)

// tokenStore stores tokens in Redis using the project's Redis cache helper.
type tokenStore struct {
	cache  cache.Cache[string, storage.TokenMetadata]
	secret []byte
}

// NewRedisTokenStore constructs a RedisTokenStore. The caller must provide a
// redis.Client and a secret key used to HMAC-sign tokens.
func NewRedisTokenStore(client *redis.Client, secret []byte) *tokenStore {
	c := rediscache.New[string, storage.TokenMetadata](client)
	// assign to the generic cache.Cache interface
	var ci cache.Cache[string, storage.TokenMetadata] = c
	return &tokenStore{cache: ci, secret: secret}
}

// NewTokenStore constructs a TokenStore from any implementation of
// cache.Cache. This allows passing in in-memory or other cache implementations
// for testing or alternative backends.
func NewTokenStore(c cache.Cache[string, storage.TokenMetadata], secret []byte) *tokenStore {
	return &tokenStore{cache: c, secret: secret}
}

// hmacToken returns the hex-encoded HMAC of the provided token string.
func (r *tokenStore) hmacToken(token string) string {
	mac := hmac.New(sha256.New, r.secret)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// CreateToken stores token metadata under the HMAC(token) key with TTL.
func (r *tokenStore) CreateToken(
	ctx context.Context,
	token string,
	meta storage.TokenMetadata,
	ttl time.Duration,
) error {
	if r == nil || r.cache == nil {
		return errors.New("redis token store not configured")
	}
	key := r.hmacToken(token)
	return r.cache.Set(ctx, key, meta, ttl)
}

// ConsumeToken retrieves and deletes the token metadata atomically if present.
func (r *tokenStore) ConsumeToken(ctx context.Context, token string) (*storage.TokenMetadata, error) {
	if r == nil || r.cache == nil {
		return nil, errors.New("redis token store not configured")
	}
	key := r.hmacToken(token)
	// Atomically get and delete the metadata using the cache helper
	meta, err := r.cache.GetAndDelete(ctx, key)
	if err != nil {
		if err == cache.Nil {
			return nil, nil
		}
		return nil, err
	}
	return &meta, nil
}
