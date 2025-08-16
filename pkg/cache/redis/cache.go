package rediscache

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/yuisofull/goload/pkg/cache"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache[K comparable, V any] struct {
	client     *redis.Client
	marshaler  Marshaler[V]
	keyEncoder KeyEncoder[K]
}

type Option[K comparable, V any] func(*RedisCache[K, V])

func WithMarshaler[K comparable, V any](marshaler Marshaler[V]) Option[K, V] {
	return func(c *RedisCache[K, V]) {
		c.marshaler = marshaler
	}
}

func WithKeyEncoder[K comparable, V any](keyEncoder KeyEncoder[K]) Option[K, V] {
	return func(c *RedisCache[K, V]) {
		c.keyEncoder = keyEncoder
	}
}

func New[K comparable, V any](client *redis.Client, opts ...Option[K, V]) *RedisCache[K, V] {
	redisCache := &RedisCache[K, V]{
		client: client,
	}
	for _, opt := range opts {
		opt(redisCache)
	}

	redisCache.marshaler = cmp.Or[Marshaler[V]](redisCache.marshaler, &DefaultMarshaler[V]{})
	redisCache.keyEncoder = cmp.Or[KeyEncoder[K]](redisCache.keyEncoder, &DefaultKeyEncoder[K]{})

	return redisCache
}

func (c *RedisCache[K, V]) Set(ctx context.Context, key K, value V, ttl time.Duration) error {
	data, err := c.marshaler.Marshal(value)
	if err != nil {
		return err
	}
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, encodedKey, data, ttl).Err()
}

func (c *RedisCache[K, V]) Get(ctx context.Context, key K) (V, error) {
	var zero V
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return zero, err
	}
	val, err := c.client.Get(ctx, encodedKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return zero, cache.Nil
	}
	if err != nil {
		return zero, err
	}
	v, err := c.marshaler.Unmarshal(val)
	if err != nil {
		return zero, err
	}
	return v, nil
}

func (c *RedisCache[K, V]) Delete(ctx context.Context, key K) error {
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return err
	}
	return c.client.Del(ctx, encodedKey).Err()
}

func (c *RedisCache[K, V]) Has(ctx context.Context, key K) (bool, error) {
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return false, err
	}
	exists, err := c.client.Exists(ctx, encodedKey).Result()
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (c *RedisCache[K, V]) Clear(ctx context.Context) error {
	prefix := c.keyEncoder.Namespace()
	keys, err := c.client.Keys(ctx, prefix+":*").Result()
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		return c.client.Del(ctx, keys...).Err()
	}
	return nil
}

func (c *RedisCache[K, V]) Len(ctx context.Context) (int, error) {
	prefix := c.keyEncoder.Namespace()
	keys, err := c.client.Keys(ctx, prefix+":*").Result()
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}

func (c *RedisCache[K, V]) Add(ctx context.Context, key K, members ...V) error {
	vals := make([]interface{}, len(members))
	for i, m := range members {
		b, err := c.marshaler.Marshal(m)
		if err != nil {
			return err
		}
		vals[i] = b
	}
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return fmt.Errorf("failed to encode key: %w", err)
	}
	return c.client.SAdd(ctx, encodedKey, vals...).Err()
}

func (c *RedisCache[K, V]) Remove(ctx context.Context, key K, members ...V) error {
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return fmt.Errorf("failed to encode key: %w", err)
	}
	vals := make([]interface{}, len(members))
	for i, m := range members {
		b, err := c.marshaler.Marshal(m)
		if err != nil {
			return err
		}
		vals[i] = b
	}
	return c.client.SRem(ctx, encodedKey, vals...).Err()
}

func (c *RedisCache[K, V]) Contains(ctx context.Context, key K, member V) (bool, error) {
	b, err := c.marshaler.Marshal(member)
	if err != nil {
		return false, err
	}
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return false, fmt.Errorf("failed to encode key: %w", err)
	}
	return c.client.SIsMember(ctx, encodedKey, b).Result()
}

func (c *RedisCache[K, V]) Members(ctx context.Context, key K) ([]V, error) {
	encodedKey, err := c.keyEncoder.Encode(key)
	if err != nil {
		return nil, fmt.Errorf("failed to encode key: %w", err)
	}
	raw, err := c.client.SMembers(ctx, encodedKey).Result()
	if err != nil {
		return nil, err
	}

	result := make([]V, 0, len(raw))
	for _, str := range raw {
		v, err := c.marshaler.Unmarshal([]byte(str))
		if err != nil {
			return result, err
		}
		result = append(result, v)
	}
	return result, nil
}
