package middleware

import (
	"sync"
	"time"

	"github.com/devravik/go-license-api/internal/infrastructure/ratelimit"
	"github.com/gofiber/fiber/v3"
)

const maxBuckets = 10_000

type RateLimiter struct {
	mu        sync.RWMutex
	buckets   map[string]*ratelimit.Bucket
	idleEvict time.Duration
}

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		buckets:   make(map[string]*ratelimit.Bucket),
		idleEvict: 5 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		tenant := TenantFromCtx(c)
		if tenant == nil {
			return c.Next()
		}
		bucket := rl.getOrCreate(tenant.ID, tenant.RPS, tenant.Burst)
		if !bucket.Allow() {
			return c.Status(fiber.StatusTooManyRequests).JSON(ErrorResponse{
				Valid: false,
				Error: "rate_limit_exceeded",
			})
		}
		return c.Next()
	}
}

func (rl *RateLimiter) Invalidate(tenantID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, tenantID)
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for id, b := range rl.buckets {
			if time.Since(b.LastSeen()) > rl.idleEvict {
				delete(rl.buckets, id)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) getOrCreate(tenantID string, rps, burst int) *ratelimit.Bucket {
	rl.mu.RLock()
	b, ok := rl.buckets[tenantID]
	rl.mu.RUnlock()
	if ok {
		return b
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok = rl.buckets[tenantID]; ok {
		return b
	}
	if len(rl.buckets) >= maxBuckets {
		return ratelimit.NewBucket(rps, burst)
	}
	b = ratelimit.NewBucket(rps, burst)
	rl.buckets[tenantID] = b
	return b
}
