package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/devravik/go-license-api/internal/infrastructure/ratelimit"
	"github.com/gofiber/fiber/v3"
)

type PublicLicenseRateLimiter struct {
	mu        sync.RWMutex
	buckets   map[string]*ratelimit.Bucket
	idleEvict time.Duration
	// limits
	licRPS   int
	licBurst int
	ipRPS    int
	ipBurst  int
	pathRPS  int
	pathBurst int
}

func NewPublicLicenseRateLimiter() *PublicLicenseRateLimiter {
	rl := &PublicLicenseRateLimiter{
		buckets:   make(map[string]*ratelimit.Bucket),
		idleEvict: 5 * time.Minute,
		licRPS:    30,
		licBurst:  60,
		ipRPS:     5,
		ipBurst:   10,
		pathRPS:   200,
		pathBurst: 400,
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *PublicLicenseRateLimiter) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		// Global path bucket for burst protection
		pathKey := "path:" + c.Method() + ":" + c.Route().Path
		if !rl.getOrCreate(pathKey, rl.pathRPS, rl.pathBurst).Allow() {
			return c.Status(fiber.StatusTooManyRequests).JSON(errorResponse("rate_limit_exceeded", "Rate limit exceeded"))
		}
		// Primary license key bucket
		if licKey := extractLicenseKey(c); licKey != "" {
			h := sha256.Sum256([]byte(licKey))
			licBucketKey := "lic:" + hex.EncodeToString(h[:])
			if !rl.getOrCreate(licBucketKey, rl.licRPS, rl.licBurst).Allow() {
				return c.Status(fiber.StatusTooManyRequests).JSON(errorResponse("rate_limit_exceeded", "Rate limit exceeded"))
			}
			return c.Next()
		}
		// Fallback to client IP
		ip := strings.TrimSpace(c.IP())
		if ip == "" {
			ip = "unknown"
		}
		if !rl.getOrCreate("ip:"+ip, rl.ipRPS, rl.ipBurst).Allow() {
			return c.Status(fiber.StatusTooManyRequests).JSON(errorResponse("rate_limit_exceeded", "Rate limit exceeded"))
		}
		return c.Next()
	}
}

func (rl *PublicLicenseRateLimiter) cleanupLoop() {
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

func (rl *PublicLicenseRateLimiter) getOrCreate(key string, rps, burst int) *ratelimit.Bucket {
	rl.mu.RLock()
	b, ok := rl.buckets[key]
	rl.mu.RUnlock()
	if ok {
		return b
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok = rl.buckets[key]; ok {
		return b
	}
	b = ratelimit.NewBucket(rps, burst)
	rl.buckets[key] = b
	return b
}

type keyBody struct {
	Key string `json:"key"`
}

func extractLicenseKey(c fiber.Ctx) string {
	if c.Method() == fiber.MethodGet {
		// Preferred param
		if v := strings.TrimSpace(c.Params("license_key")); v != "" {
			return v
		}
		// Legacy alias
		if v := strings.TrimSpace(c.Params("key")); v != "" {
			return v
		}
		return ""
	}
	// For JSON bodies, perform a tiny unmarshal into {key}
	body := c.Body()
	if len(body) == 0 {
		return ""
	}
	if len(body) > 2048 {
		// Avoid heavy parsing for very large bodies
		return ""
	}
	var kb keyBody
	if err := json.Unmarshal(body, &kb); err != nil {
		return ""
	}
	return strings.TrimSpace(kb.Key)
}

