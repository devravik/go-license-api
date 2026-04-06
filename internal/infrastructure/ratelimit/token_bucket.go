package ratelimit

import (
	"sync"
	"time"
)

type Bucket struct {
	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	rps      float64
	lastFill time.Time
	lastSeen time.Time

	now func() time.Time
}

func NewBucket(rps, burst int) *Bucket {
	now := time.Now
	t := now()
	return &Bucket{
		tokens:   float64(burst),
		maxBurst: float64(burst),
		rps:      float64(rps),
		lastFill: t,
		lastSeen: t,
		now:      now,
	}
}

// NewBucketWithNow creates a bucket with a deterministic clock function.
// This is intended for unit testing; production code should use NewBucket.
func NewBucketWithNow(rps, burst int, nowFn func() time.Time) *Bucket {
	if nowFn == nil {
		nowFn = time.Now
	}
	t := nowFn()
	return &Bucket{
		tokens:   float64(burst),
		maxBurst: float64(burst),
		rps:      float64(rps),
		lastFill: t,
		lastSeen: t,
		now:      nowFn,
	}
}

func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.lastFill = now
	b.lastSeen = now

	b.tokens += elapsed * b.rps
	if b.tokens > b.maxBurst {
		b.tokens = b.maxBurst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *Bucket) LastSeen() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastSeen
}
