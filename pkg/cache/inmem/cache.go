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
	mu     sync.RWMutex
	data   map[K]item[V]
	ticker *time.Ticker
	stop   chan struct{}
}

// New creates a new memory cache with a background reaper.
func New[K comparable, V any](reapInterval time.Duration) (cache *Cache[K, V], closeFunc func()) {
	c := &Cache[K, V]{
		data:   make(map[K]item[V]),
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
	c.mu.Lock()
	defer c.mu.Unlock()

	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	c.data[key] = item[V]{value, exp}
}

func (c *Cache[K, V]) Get(_ context.Context, key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var zero V
	it, ok := c.data[key]
	if !ok {
		return zero, false
	}
	if !it.expiration.IsZero() && time.Now().After(it.expiration) {
		return zero, false
	}
	return it.value, true
}

func (c *Cache[K, V]) Delete(_ context.Context, key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

func (c *Cache[K, V]) Has(ctx context.Context, key K) bool {
	_, ok := c.Get(ctx, key)
	return ok
}

func (c *Cache[K, V]) Clear(_ context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[K]item[V])
}

func (c *Cache[K, V]) Len(_ context.Context) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

func (c *Cache[K, V]) reap() {
	for {
		select {
		case <-c.ticker.C:
			now := time.Now()
			c.mu.Lock()
			for k, v := range c.data {
				if !v.expiration.IsZero() && v.expiration.Before(now) {
					delete(c.data, k)
				}
			}
			c.mu.Unlock()
		case <-c.stop:
			return
		}
	}
}
