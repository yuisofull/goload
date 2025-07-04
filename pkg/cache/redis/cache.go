package rediscache

import (
	"bytes"
	"context"
	"encoding/gob"
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
	return fmt.Sprintf("%s:%T:%v", c.prefix, k, k)
}

func encode[V any](v V) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(v)
	return buf.Bytes(), err
}

func decode[V any](data []byte) (V, error) {
	var v V
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&v)
	return v, err
}

func (c *RedisCache[K, V]) Set(ctx context.Context, key K, value V, ttl time.Duration) {
	data, err := encode(value)
	if err != nil {
		return
	}
	c.client.Set(ctx, c.key(key), data, ttl)
}

func (c *RedisCache[K, V]) Get(ctx context.Context, key K) (V, bool) {
	var zero V

	val, err := c.client.Get(ctx, c.key(key)).Bytes()
	if errors.Is(err, redis.Nil) || err != nil {
		return zero, false
	}

	v, err := decode[V](val)
	if err != nil {
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

func (c *RedisCache[K, V]) Add(ctx context.Context, key K, members ...V) error {
	vals := make([]interface{}, len(members))
	for i, m := range members {
		b, err := encode(m)
		if err != nil {
			return err
		}
		vals[i] = b
	}
	return c.client.SAdd(ctx, c.key(key), vals...).Err()
}

func (c *RedisCache[K, V]) Remove(ctx context.Context, key K, members ...V) error {
	vals := make([]interface{}, len(members))
	for i, m := range members {
		b, err := encode(m)
		if err != nil {
			return err
		}
		vals[i] = b
	}
	return c.client.SRem(ctx, c.key(key), vals...).Err()
}

func (c *RedisCache[K, V]) Contains(ctx context.Context, key K, member V) (bool, error) {
	b, err := encode(member)
	if err != nil {
		return false, err
	}
	return c.client.SIsMember(ctx, c.key(key), b).Result()
}

func (c *RedisCache[K, V]) Members(ctx context.Context, key K) ([]V, error) {
	raw, err := c.client.SMembers(ctx, c.key(key)).Result()
	if err != nil {
		return nil, err
	}

	result := make([]V, 0, len(raw))
	for _, str := range raw {
		v, err := decode[V]([]byte(str))
		if err != nil {
			continue // skip bad entry
		}
		result = append(result, v)
	}
	return result, nil
}
