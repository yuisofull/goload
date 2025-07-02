package rediscache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache[K comparable, V any] struct {
	client *redis.Client
	prefix string
}

func New[K comparable, V any](client *redis.Client, prefix string) *RedisCache[K, V] {
	return &RedisCache[K, V]{
		client: client,
		prefix: prefix,
	}
}

func (c *RedisCache[K, V]) key(k K) string {
	return fmt.Sprintf("%s:%v", c.prefix, k)
}

func (c *RedisCache[K, V]) Set(ctx context.Context, key K, value V, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	c.client.Set(ctx, c.key(key), data, ttl)
}

func (c *RedisCache[K, V]) Get(ctx context.Context, key K) (V, bool) {
	var zero V

	val, err := c.client.Get(ctx, c.key(key)).Result()
	if errors.Is(err, redis.Nil) || err != nil {
		return zero, false
	}

	var v V
	if err := json.Unmarshal([]byte(val), &v); err != nil {
		return zero, false
	}
	return v, true
}

func (c *RedisCache[K, V]) Delete(ctx context.Context, key K) {
	c.client.Del(ctx, c.key(key))
}

func (c *RedisCache[K, V]) Has(ctx context.Context, key K) bool {
	exists, err := c.client.Exists(ctx, c.key(key)).Result()
	return err == nil && exists == 1
}

func (c *RedisCache[K, V]) Clear(ctx context.Context) {
	// Keys is not recommended for production use. See https://redis.io/commands/keys/
	keys, err := c.client.Keys(ctx, c.prefix+":*").Result()
	if err == nil && len(keys) > 0 {
		c.client.Del(ctx, keys...)
	}
}

func (c *RedisCache[K, V]) Len(ctx context.Context) int {
	keys, err := c.client.Keys(ctx, c.prefix+":*").Result()
	if err != nil {
		return 0
	}
	return len(keys)
}
