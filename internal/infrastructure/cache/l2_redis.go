package cache

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/redis/go-redis/v9"
)

const redisPrefix = "go-license-api:"

type redisLicense struct {
	License   domain.License `json:"license"`
	ExpiresAt time.Time      `json:"expires_at"`
}

type redisTenant struct {
	Tenant    domain.Tenant `json:"tenant"`
	ExpiresAt time.Time     `json:"expires_at"`
}

type redisProduct struct {
	Product   domain.Product `json:"product"`
	ExpiresAt time.Time      `json:"expires_at"`
}
type redisPlan struct {
	Plan      domain.Plan `json:"plan"`
	ExpiresAt time.Time   `json:"expires_at"`
}
type redisNegative struct {
	Negative  bool      `json:"negative"`
	ExpiresAt time.Time `json:"expires_at"`
}

// L2Cache is an optional Redis-backed cache layer.
// It includes an atomic circuit breaker to auto-disable L2 on repeated failures.
type L2Cache struct {
	client *redis.Client

	enabled  atomic.Bool
	failures atomic.Int32
	lastFail atomic.Int64 // unix nano
}

func NewL2Cache(redisURL string) (*L2Cache, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	// Tight timeouts to avoid blocking workers.
	opt.DialTimeout = 200 * time.Millisecond
	opt.ReadTimeout = 100 * time.Millisecond
	opt.WriteTimeout = 100 * time.Millisecond

	c := &L2Cache{client: redis.NewClient(opt)}
	c.enabled.Store(true)
	return c, nil
}

func (c *L2Cache) isAvailable() bool {
	if c.enabled.Load() {
		return true
	}
	last := time.Unix(0, c.lastFail.Load())
	if time.Since(last) > time.Minute {
		c.failures.Store(0)
		c.enabled.Store(true)
		return true
	}
	return false
}

func (c *L2Cache) recordFailure() {
	if c.failures.Add(1) > 5 {
		c.enabled.Store(false)
		c.lastFail.Store(time.Now().UnixNano())
	}
}

func (c *L2Cache) recordSuccess() {
	c.failures.Store(0)
}

func (c *L2Cache) Get(ctx context.Context, key string) (*CacheEntry, bool) {
	if !c.isAvailable() {
		return nil, false
	}

	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		// redis.Nil is a normal cache miss.
		if err != redis.Nil {
			c.recordFailure()
		}
		return nil, false
	}
	c.recordSuccess()

	// 1) Decode negative payload
	var neg redisNegative
	if err := json.Unmarshal(data, &neg); err == nil && neg.Negative {
		if time.Now().After(neg.ExpiresAt) {
			return nil, false
		}
		return &CacheEntry{Negative: true, ExpiresAt: neg.ExpiresAt}, true
	}

	// 2) Decode license payload
	var payload redisLicense
	if err := json.Unmarshal(data, &payload); err != nil {
		// Might be another payload type; fall through.
	} else {
		if time.Now().After(payload.ExpiresAt) {
			return nil, false
		}
		return &CacheEntry{Value: &payload.License, ExpiresAt: payload.ExpiresAt}, true
	}

	// 3) Decode tenant payload
	var tp redisTenant
	if err := json.Unmarshal(data, &tp); err != nil {
		// 4) Try product payload
		var pp redisProduct
		if err := json.Unmarshal(data, &pp); err != nil {
			// 5) Try plan payload
			var pl redisPlan
			if err := json.Unmarshal(data, &pl); err != nil {
				return nil, false
			}
			if time.Now().After(pl.ExpiresAt) {
				return nil, false
			}
			return &CacheEntry{Value: &pl.Plan, ExpiresAt: pl.ExpiresAt}, true
		}
		if time.Now().After(pp.ExpiresAt) {
			return nil, false
		}
		return &CacheEntry{Value: &pp.Product, ExpiresAt: pp.ExpiresAt}, true
	}
	if time.Now().After(tp.ExpiresAt) {
		return nil, false
	}
	return &CacheEntry{Value: &tp.Tenant, ExpiresAt: tp.ExpiresAt}, true
}

func (c *L2Cache) Set(ctx context.Context, key string, value *CacheEntry, ttl time.Duration) {
	if !c.isAvailable() {
		return
	}

	value.ExpiresAt = time.Now().Add(ttl)

	if value.Negative {
		payload := redisNegative{Negative: true, ExpiresAt: value.ExpiresAt}
		data, err := json.Marshal(&payload)
		if err != nil {
			return
		}
		if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
			c.recordFailure()
			return
		}
		c.recordSuccess()
		return
	}

	lic, ok := value.Value.(*domain.License)
	if ok && lic != nil {
		payload := redisLicense{License: *lic, ExpiresAt: value.ExpiresAt}
		data, err := json.Marshal(&payload)
		if err != nil {
			return
		}
		if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
			c.recordFailure()
			return
		}
		c.recordSuccess()
		return
	}

	t, ok := value.Value.(*domain.Tenant)
	if !ok || t == nil {
		// Try product
		p, ok := value.Value.(*domain.Product)
		if !ok || p == nil {
			// Try plan
			pl, ok := value.Value.(*domain.Plan)
			if !ok || pl == nil {
				return
			}
			planPayload := redisPlan{Plan: *pl, ExpiresAt: value.ExpiresAt}
			data, err := json.Marshal(&planPayload)
			if err != nil {
				return
			}
			if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
				c.recordFailure()
				return
			}
			c.recordSuccess()
			return
		}
		pp := redisProduct{Product: *p, ExpiresAt: value.ExpiresAt}
		data, err := json.Marshal(&pp)
		if err != nil {
			return
		}
		if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
			c.recordFailure()
			return
		}
		c.recordSuccess()
		return
	}

	tp := redisTenant{Tenant: *t, ExpiresAt: value.ExpiresAt}
	data, err := json.Marshal(&tp)
	if err != nil {
		return
	}
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		c.recordFailure()
		return
	}
	c.recordSuccess()
}

func (c *L2Cache) Publish(ctx context.Context, channel, payload string) {
	if !c.isAvailable() {
		return
	}
	_ = c.client.Publish(ctx, channel, payload).Err()
}

func (c *L2Cache) Subscribe(ctx context.Context, channel string, handler func(string)) {
	go func() {
		for {
			// Respect circuit breaker cooldown to avoid a tight reconnect loop when Redis is down.
			if !c.isAvailable() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			pubsub := c.client.Subscribe(ctx, channel)
			for msg := range pubsub.Channel() {
				handler(msg.Payload)
			}
			_ = pubsub.Close()

			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

func (c *L2Cache) Invalidate(ctx context.Context, _ string, key string) {
	if !c.isAvailable() {
		return
	}
	_ = c.client.Del(ctx, key).Err()
}

func (c *L2Cache) InvalidateAll(ctx context.Context, prefix string) error {
	if !c.isAvailable() {
		return nil // best-effort; caller already invalidated L1
	}

	pipe := c.client.Pipeline()

	iter := c.client.Scan(ctx, 0, prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		pipe.Del(ctx, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	_, err := pipe.Exec(ctx)
	return err
}
