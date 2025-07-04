package inmem

import (
	"context"
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
func New[K comparable, V any](reapInterval time.Duration) (cache *Cache[K, V], closeFunc func()) {
	c := &Cache[K, V]{
		ticker: time.NewTicker(reapInterval),
		stop:   make(chan struct{}),
	}
	go c.reap()
	return c, func() {
		c.ticker.Stop()
		close(c.stop)
	}
}

func (c *Cache[K, V]) Set(_ context.Context, key K, value V, ttl time.Duration) {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	c.data.Store(key, item[V]{value: value, expiration: exp})
}

func (c *Cache[K, V]) Get(_ context.Context, key K) (V, bool) {
	var zero V
	val, ok := c.data.Load(key)
	if !ok {
		return zero, false
	}
	it := val.(item[V])
	if !it.expiration.IsZero() && time.Now().After(it.expiration) {
		c.data.Delete(key)
		return zero, false
	}
	return it.value, true
}

func (c *Cache[K, V]) Delete(_ context.Context, key K) {
	c.data.Delete(key)
}

func (c *Cache[K, V]) Has(ctx context.Context, key K) bool {
	_, ok := c.Get(ctx, key)
	return ok
}

func (c *Cache[K, V]) Clear(_ context.Context) {
	c.data.Range(func(key, _ any) bool {
		c.data.Delete(key)
		return true
	})
}

func (c *Cache[K, V]) Len(_ context.Context) int {
	count := 0
	c.data.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
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
