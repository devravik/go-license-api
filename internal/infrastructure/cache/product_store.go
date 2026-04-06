package cache

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

// ProductStore is cache-only on Get(); it never queries PostgreSQL.
// Use in data plane when product attributes are needed in validation.
type ProductStore struct {
	l1 *L1Cache
	l2 *L2Cache // optional

	ttl         time.Duration
	ttlNegative time.Duration
}

func NewProductStore(l1 *L1Cache, l2 *L2Cache, ttl, ttlNegative time.Duration) *ProductStore {
	s := &ProductStore{l1: l1, l2: l2, ttl: ttl, ttlNegative: ttlNegative}
	go s.cleanupLoop()
	return s
}

func (s *ProductStore) cacheKey(tenantID, code string) string {
	return cacheKey(tenantID, "product:"+code)
}

// Get is cache-only; miss → invalid product (no DB call).
func (s *ProductStore) Get(ctx context.Context, tenantID, code string) (*domain.Product, error) {
	ck := s.cacheKey(tenantID, code)

	if entry, ok := s.l1.Get(ctx, ck); ok {
		if entry.Negative {
			return nil, domain.ErrProductNotFound
		}
		p, ok := entry.Value.(*domain.Product)
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
			p, ok := entry.Value.(*domain.Product)
			if !ok || p == nil {
				return nil, domain.ErrProductNotFound
			}
			s.l1.Set(ctx, ck, &CacheEntry{Value: p}, s.ttl)
			return p, nil
		}
	}

	// Full miss: negative-cache briefly in L1 if below high-water mark.
	if s.l1.Len() < (s.l1.MaxEntries()*9)/10 {
		s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
	}
	return nil, domain.ErrProductNotFound
}

// Set performs write-through for product updates.
func (s *ProductStore) Set(ctx context.Context, tenantID, code string, product *domain.Product) {
	ck := s.cacheKey(tenantID, code)
	s.l1.Set(ctx, ck, &CacheEntry{Value: product}, s.ttl)
	if s.l2 != nil {
		s.l2.Set(ctx, ck, &CacheEntry{Value: product}, s.ttl)
	}
}

// Invalidate removes a single product entry and publishes invalidation to other instances.
func (s *ProductStore) Invalidate(ctx context.Context, tenantID, code string) {
	ck := s.cacheKey(tenantID, code)
	s.l1.Invalidate(ctx, "", ck)
	if s.l2 != nil {
		s.l2.Invalidate(ctx, "", ck)
		s.l2.Publish(ctx, "cache:invalidate", ck)
	}
}

func (s *ProductStore) SubscribeInvalidation(ctx context.Context) {
	if s.l2 == nil {
		return
	}
	s.l2.Subscribe(ctx, "cache:invalidate", func(payload string) {
		if len(payload) >= len(redisPrefix) && payload[:len(redisPrefix)] == redisPrefix {
			// For product keys, we simply pass through to L1 invalidate on exact key.
			if payload[len(payload)-1] == ':' {
				// No tenant-wide product invalidation here; keep scope precise.
				return
			}
			s.l1.Invalidate(ctx, "", payload)
		}
	})
}

func (s *ProductStore) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.l1.CleanupExpired(1000)
	}
}
