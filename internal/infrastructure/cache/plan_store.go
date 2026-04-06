package cache

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

// PlanStore is cache-only on Get(); it never queries PostgreSQL.
type PlanStore struct {
	l1 *L1Cache
	l2 *L2Cache // optional

	ttl         time.Duration
	ttlNegative time.Duration
}

func NewPlanStore(l1 *L1Cache, l2 *L2Cache, ttl, ttlNegative time.Duration) *PlanStore {
	s := &PlanStore{l1: l1, l2: l2, ttl: ttl, ttlNegative: ttlNegative}
	go s.cleanupLoop()
	return s
}

func (s *PlanStore) cacheKey(tenantID, planID string) string {
	return cacheKey(tenantID, "plan:"+planID)
}

func (s *PlanStore) Get(ctx context.Context, tenantID, planID string) (*domain.Plan, error) {
	ck := s.cacheKey(tenantID, planID)

	if entry, ok := s.l1.Get(ctx, ck); ok {
		if entry.Negative {
			return nil, domain.ErrProductNotFound
		}
		p, ok := entry.Value.(*domain.Plan)
		if !ok || p == nil {
			return nil, domain.ErrProductNotFound
		}
		return p, nil
	}

	if s.l2 != nil {
		if entry, ok := s.l2.Get(ctx, ck); ok {
			if entry.Negative {
				s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
				return nil, domain.ErrProductNotFound
			}
			p, ok := entry.Value.(*domain.Plan)
			if !ok || p == nil {
				return nil, domain.ErrProductNotFound
			}
			s.l1.Set(ctx, ck, &CacheEntry{Value: p}, s.ttl)
			return p, nil
		}
	}

	if s.l1.Len() < (s.l1.MaxEntries()*9)/10 {
		s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
	}
	return nil, domain.ErrProductNotFound
}

func (s *PlanStore) Set(ctx context.Context, tenantID, planID string, plan *domain.Plan) {
	ck := s.cacheKey(tenantID, planID)
	s.l1.Set(ctx, ck, &CacheEntry{Value: plan}, s.ttl)
	if s.l2 != nil {
		s.l2.Set(ctx, ck, &CacheEntry{Value: plan}, s.ttl)
	}
}

func (s *PlanStore) Invalidate(ctx context.Context, tenantID, planID string) {
	ck := s.cacheKey(tenantID, planID)
	s.l1.Invalidate(ctx, "", ck)
	if s.l2 != nil {
		s.l2.Invalidate(ctx, "", ck)
		s.l2.Publish(ctx, "cache:invalidate", ck)
	}
}

func (s *PlanStore) SubscribeInvalidation(ctx context.Context) {
	if s.l2 == nil {
		return
	}
	s.l2.Subscribe(ctx, "cache:invalidate", func(payload string) {
		if len(payload) >= len(redisPrefix) && payload[:len(redisPrefix)] == redisPrefix {
			if payload[len(payload)-1] == ':' {
				return
			}
			s.l1.Invalidate(ctx, "", payload)
		}
	})
}

func (s *PlanStore) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.l1.CleanupExpired(1000)
	}
}
