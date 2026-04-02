package cache

import (
	"context"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// L1Cache is a bounded, thread-safe in-memory LRU cache.
// hashicorp/golang-lru is concurrency-safe.
type L1Cache struct {
	lru        *lru.Cache[string, *CacheEntry]
	maxEntries int
}

func NewL1Cache(maxEntries int) (*L1Cache, error) {
	c, err := lru.New[string, *CacheEntry](maxEntries)
	if err != nil {
		return nil, err
	}
	return &L1Cache{lru: c, maxEntries: maxEntries}, nil
}

func (c *L1Cache) MaxEntries() int { return c.maxEntries }

func (c *L1Cache) Len() int { return c.lru.Len() }

func (c *L1Cache) Get(_ context.Context, key string) (*CacheEntry, bool) {
	entry, ok := c.lru.Get(key) // promotes key
	if !ok {
		return nil, false
	}
	if entry.IsExpired(time.Now()) {
		c.lru.Remove(key)
		return nil, false
	}
	return entry, true
}

func (c *L1Cache) Set(_ context.Context, key string, value *CacheEntry, ttl time.Duration) {
	// Copy to avoid mutating caller-owned pointers (safer if CacheEntry is reused/shared).
	entry := *value
	entry.ExpiresAt = time.Now().Add(ttl)
	c.lru.Add(key, &entry)
}

func (c *L1Cache) Invalidate(_ context.Context, _ string, key string) {
	c.lru.Remove(key)
}

// InvalidateAll removes all keys with the given prefix.
func (c *L1Cache) InvalidateAll(_ context.Context, prefix string) {
	for _, k := range c.lru.Keys() {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			c.lru.Remove(k)
		}
	}
}
