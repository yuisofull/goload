package cache

import (
	"context"
	"errors"
	"time"
)

type Cache[K comparable, V any] interface {
	// Set will store the value in the cache with the given key and ttl.
	Set(ctx context.Context, key K, value V, ttl time.Duration) error

	// Get will return the value stored in the cache with the given key. If the
	// value is not found, Nil is returned.
	Get(ctx context.Context, key K) (V, error)

	// Delete will remove the value stored in the cache with the given key.
	Delete(ctx context.Context, key K) error

	// Has will return true if the value is stored in the cache with the given key.
	Has(ctx context.Context, key K) (bool, error)

	// Clear will remove all values from the cache.
	Clear(ctx context.Context) error

	// Len will return the number of values stored in the cache.
	Len(ctx context.Context) (int, error)
}

type SetCache[K comparable, V any] interface {
	Add(ctx context.Context, key K, members ...V) error
	Remove(ctx context.Context, key K, members ...V) error
	Members(ctx context.Context, key K) ([]V, error)
	Contains(ctx context.Context, key K, member V) (bool, error)
}

var (
	// Nil is return when the value does not exist
	Nil = errors.New("cache: nil")
)
