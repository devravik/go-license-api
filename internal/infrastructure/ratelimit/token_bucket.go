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
}

func NewBucket(rps, burst int) *Bucket {
	now := time.Now()
	return &Bucket{
		tokens:   float64(burst),
		maxBurst: float64(burst),
		rps:      float64(rps),
		lastFill: now,
		lastSeen: now,
	}
}

func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
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
