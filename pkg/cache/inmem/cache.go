package inmem

import (
	"context"
	"github.com/yuisofull/goload/pkg/cache"
	"sync"
	"time"
)

type item[V any] struct {
	value      V
	expiration time.Time
}

type Cache[K comparable, V any] struct {
	data   sync.Map // key: K, value: item[V]
	ticker *time.Ticker
	stop   chan struct{}
}

// New creates a new memory cache with a background reaper.
func New[K comparable, V any](reapInterval time.Duration) (cache *Cache[K, V]) {
	c := &Cache[K, V]{
		ticker: time.NewTicker(reapInterval),
		stop:   make(chan struct{}),
	}
	go c.reap()
	return c
}

func (c *Cache[K, V]) Set(_ context.Context, key K, value V, ttl time.Duration) error {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	c.data.Store(key, item[V]{value: value, expiration: exp})
	return nil
}

func (c *Cache[K, V]) Get(_ context.Context, key K) (V, error) {
	var zero V
	val, ok := c.data.Load(key)
	if !ok {
		return zero, cache.Nil
	}
	it := val.(item[V])
	if !it.expiration.IsZero() && time.Now().After(it.expiration) {
		c.data.Delete(key)
		return zero, cache.Nil
	}
	return it.value, nil
}

func (c *Cache[K, V]) Delete(_ context.Context, key K) error {
	c.data.Delete(key)
	return nil
}

func (c *Cache[K, V]) Has(ctx context.Context, key K) (bool, error) {
	_, err := c.Get(ctx, key)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cache[K, V]) Clear(_ context.Context) error {
	c.data.Range(func(key, _ any) bool {
		c.data.Delete(key)
		return true
	})
	return nil
}

func (c *Cache[K, V]) Len(_ context.Context) (int, error) {
	count := 0
	c.data.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count, nil
}

func (c *Cache[K, V]) reap() {
	for {
		select {
		case <-c.ticker.C:
			now := time.Now()
			c.data.Range(func(key, value any) bool {
				it := value.(item[V])
				if !it.expiration.IsZero() && it.expiration.Before(now) {
					c.data.Delete(key)
				}
				return true
			})
		case <-c.stop:
			return
		}
	}
}

func (c *Cache[K, V]) Close() {
	close(c.stop)
	c.ticker.Stop()
}
