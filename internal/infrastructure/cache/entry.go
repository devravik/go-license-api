package cache

import "time"

// CacheEntry wraps a cached value with expiry and optional negative caching.
// Negative entries represent a cached "not found/invalid" result.
type CacheEntry struct {
	Value     any
	ExpiresAt time.Time
	Negative  bool
}

func (e *CacheEntry) IsExpired(now time.Time) bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return now.After(e.ExpiresAt)
}
