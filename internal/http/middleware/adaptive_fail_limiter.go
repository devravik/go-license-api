package middleware

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/devravik/go-license-api/internal/setup"
	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
)

type blockState struct {
	until time.Time
}

type localFailState struct {
	count       int
	windowStart time.Time
}

type localStrikeState struct {
	count     int
	resetDay  int
	resetYear int
}

type AdaptiveFailLimiter struct {
	cfg         *setup.LimiterConfig
	redisClient *redis.Client

	localMu       sync.RWMutex
	localBlocks   map[string]blockState
	localFails    map[string]localFailState
	localGlobal   map[string]localFailState
	localStrikes  map[string]localStrikeState
	lastLocalPrune time.Time
}

func NewAdaptiveFailLimiter(cfg *setup.LimiterConfig, fallbackRedisURL string) *AdaptiveFailLimiter {
	if cfg == nil {
		return nil
	}
	limiter := &AdaptiveFailLimiter{
		cfg:          cfg,
		localBlocks:  make(map[string]blockState, 2048),
		localFails:   make(map[string]localFailState, 2048),
		localGlobal:  make(map[string]localFailState, 2048),
		localStrikes: make(map[string]localStrikeState, 2048),
	}
	redisURL := strings.TrimSpace(cfg.RedisURL)
	if redisURL == "" {
		redisURL = strings.TrimSpace(fallbackRedisURL)
	}
	if cfg.RedisEnabled && redisURL != "" {
		if opt, err := redis.ParseURL(redisURL); err == nil {
			opt.DialTimeout = 100 * time.Millisecond
			opt.ReadTimeout = 50 * time.Millisecond
			opt.WriteTimeout = 50 * time.Millisecond
			limiter.redisClient = redis.NewClient(opt)
		}
	}
	return limiter
}

func (l *AdaptiveFailLimiter) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if l == nil || l.cfg == nil || !l.cfg.Enabled {
			return c.Next()
		}
		ip := strings.TrimSpace(c.IP())
		tenantID := strings.TrimSpace(c.Get("X-Tenant-ID"))
		if ip == "" {
			return c.Next()
		}
		if tenantID == "" && !l.cfg.TrackUnknownTenant {
			return c.Next()
		}
		if tenantID == "" {
			tenantID = "unknown"
		}
		key := l.combinedKey(ip, tenantID)
		blocked, err := l.isBlocked(c.Context(), key)
		if err != nil && !l.cfg.FailOpen {
			return c.Status(fiber.StatusTooManyRequests).JSON(ErrorResponse{Valid: false, Error: "request_temporarily_blocked"})
		}
		if blocked {
			return c.Status(fiber.StatusTooManyRequests).JSON(ErrorResponse{Valid: false, Error: "ip_tenant_blocked"})
		}

		if err := c.Next(); err != nil {
			return err
		}
		if !l.shouldCountFailure(c) {
			return nil
		}
		if err := l.onFailure(c.Context(), ip, tenantID, key); err != nil && !l.cfg.FailOpen {
			return c.Status(fiber.StatusTooManyRequests).JSON(ErrorResponse{Valid: false, Error: "request_temporarily_blocked"})
		}
		return nil
	}
}

func (l *AdaptiveFailLimiter) shouldCountFailure(c fiber.Ctx) bool {
	status := c.Response().StatusCode()
	if status >= 500 {
		return false
	}
	if status == fiber.StatusUnauthorized || status == fiber.StatusForbidden {
		return true
	}
	if c.Path() != "/licenses/validate" {
		return false
	}
	if status != fiber.StatusOK {
		return false
	}
	body := strings.ToLower(string(c.Response().Body()))
	if strings.Contains(body, `"valid":false`) && !strings.Contains(body, "internal_validation_error") {
		return true
	}
	return false
}

func (l *AdaptiveFailLimiter) onFailure(ctx context.Context, ip, tenantID, key string) error {
	thresholdHit, err := l.incrementFailure(ctx, key, l.cfg.FailsPerMinute)
	if err != nil {
		return err
	}
	globalHit, err := l.incrementGlobalFailure(ctx, ip, l.cfg.GlobalFailsPerMinute)
	if err != nil {
		return err
	}
	if !thresholdHit && !globalHit {
		return nil
	}
	strike, err := l.incrementStrike(ctx, key)
	if err != nil {
		return err
	}
	duration := l.blockDurationForStrike(strike)
	if err := l.setBlock(ctx, key, duration); err != nil {
		return err
	}
	if l.cfg.LogBlocks {
		log.Printf("event=adaptive_fail_limiter blocked=true ip=%s tenant_id=%s strike=%d duration=%s", ip, tenantID, strike, duration)
	}
	return nil
}

func (l *AdaptiveFailLimiter) combinedKey(ip, tenantID string) string {
	return fmt.Sprintf("%s:%s", ip, tenantID)
}

func (l *AdaptiveFailLimiter) failKey(combined string) string {
	return fmt.Sprintf("%s:fail:%s", l.cfg.KeyPrefix, combined)
}

func (l *AdaptiveFailLimiter) blockKey(combined string) string {
	return fmt.Sprintf("%s:block:%s", l.cfg.KeyPrefix, combined)
}

func (l *AdaptiveFailLimiter) strikeKey(combined string) string {
	return fmt.Sprintf("%s:strike:%s", l.cfg.KeyPrefix, combined)
}

func (l *AdaptiveFailLimiter) globalFailKey(ip string) string {
	return fmt.Sprintf("%s:gfail:%s", l.cfg.KeyPrefix, ip)
}

func (l *AdaptiveFailLimiter) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, 120*time.Millisecond)
}

func (l *AdaptiveFailLimiter) isBlocked(ctx context.Context, key string) (bool, error) {
	now := time.Now()
	if l.cfg.LocalCacheEnabled {
		l.localMu.RLock()
		state, ok := l.localBlocks[key]
		l.localMu.RUnlock()
		if ok && now.Before(state.until) {
			return true, nil
		}
	}
	if l.redisClient == nil {
		return false, nil
	}
	rctx, cancel := l.withTimeout(ctx)
	defer cancel()
	ttl, err := l.redisClient.TTL(rctx, l.blockKey(key)).Result()
	if err != nil {
		return false, err
	}
	if ttl <= 0 {
		return false, nil
	}
	if l.cfg.LocalCacheEnabled {
		l.localMu.Lock()
		l.localBlocks[key] = blockState{until: now.Add(minDuration(ttl, l.cfg.LocalBlockTTL))}
		l.localMu.Unlock()
	}
	return true, nil
}

func (l *AdaptiveFailLimiter) incrementFailure(ctx context.Context, key string, threshold int) (bool, error) {
	if threshold <= 0 {
		return false, nil
	}
	if l.redisClient == nil {
		return l.incrementFailureLocal(key, threshold), nil
	}
	rctx, cancel := l.withTimeout(ctx)
	defer cancel()
	failKey := l.failKey(key)
	count, err := l.redisClient.Incr(rctx, failKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := l.redisClient.Expire(rctx, failKey, time.Minute).Err(); err != nil {
			return false, err
		}
	}
	return int(count) > threshold, nil
}

func (l *AdaptiveFailLimiter) incrementGlobalFailure(ctx context.Context, ip string, threshold int) (bool, error) {
	if threshold <= 0 {
		return false, nil
	}
	if l.redisClient == nil {
		return l.incrementGlobalFailureLocal(ip, threshold), nil
	}
	rctx, cancel := l.withTimeout(ctx)
	defer cancel()
	key := l.globalFailKey(ip)
	count, err := l.redisClient.Incr(rctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := l.redisClient.Expire(rctx, key, time.Minute).Err(); err != nil {
			return false, err
		}
	}
	return int(count) > threshold, nil
}

func (l *AdaptiveFailLimiter) incrementStrike(ctx context.Context, key string) (int, error) {
	if l.redisClient == nil {
		return l.incrementStrikeLocal(key), nil
	}
	rctx, cancel := l.withTimeout(ctx)
	defer cancel()
	skey := l.strikeKey(key)
	// Reset at midnight local-time by expiring strike key at next midnight.
	ttl := durationUntilNextMidnight(time.Now())
	if ttl <= 0 {
		ttl = l.cfg.StrikeTTL
	}
	count, err := l.redisClient.Incr(rctx, skey).Result()
	if err != nil {
		return 0, err
	}
	if err := l.redisClient.Expire(rctx, skey, ttl).Err(); err != nil {
		return 0, err
	}
	return int(count), nil
}

func (l *AdaptiveFailLimiter) setBlock(ctx context.Context, key string, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	now := time.Now()
	if l.cfg.LocalCacheEnabled {
		l.localMu.Lock()
		l.localBlocks[key] = blockState{until: now.Add(duration)}
		l.localMu.Unlock()
	}
	if l.redisClient == nil {
		return nil
	}
	rctx, cancel := l.withTimeout(ctx)
	defer cancel()
	return l.redisClient.Set(rctx, l.blockKey(key), 1, duration).Err()
}

func (l *AdaptiveFailLimiter) blockDurationForStrike(strike int) time.Duration {
	if len(l.cfg.BlockDurations) == 0 {
		return 5 * time.Minute
	}
	if strike <= 1 {
		return l.cfg.BlockDurations[0]
	}
	idx := strike - 1
	if idx >= len(l.cfg.BlockDurations) {
		idx = len(l.cfg.BlockDurations) - 1
	}
	return l.cfg.BlockDurations[idx]
}

func (l *AdaptiveFailLimiter) incrementFailureLocal(key string, threshold int) bool {
	l.pruneLocal()
	now := time.Now()
	l.localMu.Lock()
	defer l.localMu.Unlock()
	state := l.localFails[key]
	if state.windowStart.IsZero() || now.Sub(state.windowStart) >= time.Minute {
		state.windowStart = now
		state.count = 0
	}
	state.count++
	l.localFails[key] = state
	return state.count > threshold
}

func (l *AdaptiveFailLimiter) incrementGlobalFailureLocal(ip string, threshold int) bool {
	l.pruneLocal()
	now := time.Now()
	l.localMu.Lock()
	defer l.localMu.Unlock()
	state := l.localGlobal[ip]
	if state.windowStart.IsZero() || now.Sub(state.windowStart) >= time.Minute {
		state.windowStart = now
		state.count = 0
	}
	state.count++
	l.localGlobal[ip] = state
	return state.count > threshold
}

func (l *AdaptiveFailLimiter) incrementStrikeLocal(key string) int {
	l.pruneLocal()
	now := time.Now()
	year, day := now.Year(), now.YearDay()
	l.localMu.Lock()
	defer l.localMu.Unlock()
	state := l.localStrikes[key]
	if state.resetYear != year || state.resetDay != day {
		state.count = 0
		state.resetYear = year
		state.resetDay = day
	}
	state.count++
	l.localStrikes[key] = state
	return state.count
}

func (l *AdaptiveFailLimiter) pruneLocal() {
	now := time.Now()
	l.localMu.Lock()
	defer l.localMu.Unlock()
	if !l.lastLocalPrune.IsZero() && now.Sub(l.lastLocalPrune) < time.Minute {
		return
	}
	l.lastLocalPrune = now
	for k, b := range l.localBlocks {
		if now.After(b.until) {
			delete(l.localBlocks, k)
		}
	}
	for k, f := range l.localFails {
		if now.Sub(f.windowStart) >= time.Minute {
			delete(l.localFails, k)
		}
	}
	for k, f := range l.localGlobal {
		if now.Sub(f.windowStart) >= time.Minute {
			delete(l.localGlobal, k)
		}
	}
}

func durationUntilNextMidnight(now time.Time) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return next.Sub(now)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
