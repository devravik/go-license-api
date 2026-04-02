package cache

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

// TenantStore is cache-only on Get(); it never queries PostgreSQL.
// Tenant lookups in the validation path MUST use this store.
//
// Keying: tenant cache uses tenantID = "tenant" and key = apiKey, so keys follow tenantID:key format.
type TenantStore struct {
	l1 *L1Cache
	l2 *L2Cache // optional

	ttl         time.Duration
	ttlNegative time.Duration
}

func NewTenantStore(l1 *L1Cache, l2 *L2Cache, ttl, ttlNegative time.Duration) *TenantStore {
	return &TenantStore{l1: l1, l2: l2, ttl: ttl, ttlNegative: ttlNegative}
}

func (s *TenantStore) HasL2() bool { return s.l2 != nil }

func (s *TenantStore) cacheKey(apiKey string) string {
	return cacheKey("tenant", apiKey)
}

// GetByAPIKey is cache-only. Miss => invalid tenant.
func (s *TenantStore) GetByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error) {
	ck := s.cacheKey(apiKey)

	if entry, ok := s.l1.Get(ctx, ck); ok {
		if entry.Negative {
			return nil, domain.ErrInvalidTenant
		}
		t, ok := entry.Value.(*domain.Tenant)
		if !ok || t == nil {
			return nil, domain.ErrInvalidTenant
		}
		return t, nil
	}

	if s.l2 != nil {
		if entry, ok := s.l2.Get(ctx, ck); ok {
			if entry.Negative {
				s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
				return nil, domain.ErrInvalidTenant
			}
			t, ok := entry.Value.(*domain.Tenant)
			if !ok || t == nil {
				return nil, domain.ErrInvalidTenant
			}
			s.l1.Set(ctx, ck, &CacheEntry{Value: t}, s.ttl)
			return t, nil
		}
	}

	// Full miss: negative cache L1-only and bounded.
	if s.l1.Len() < (s.l1.MaxEntries()*9)/10 {
		s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
	}
	return nil, domain.ErrInvalidTenant
}

// Set performs write-through for tenant updates (API key rotation, status changes, etc).
func (s *TenantStore) Set(ctx context.Context, apiKey string, tenant *domain.Tenant) {
	ck := s.cacheKey(apiKey)
	s.l1.Set(ctx, ck, &CacheEntry{Value: tenant}, s.ttl)
	if s.l2 != nil {
		s.l2.Set(ctx, ck, &CacheEntry{Value: tenant}, s.ttl)
	}
}

func (s *TenantStore) Invalidate(ctx context.Context, apiKey string) {
	ck := s.cacheKey(apiKey)
	s.l1.Invalidate(ctx, "", ck)
	if s.l2 != nil {
		s.l2.Invalidate(ctx, "", ck)
		s.l2.Publish(ctx, "cache:invalidate", ck)
	}
}

func (s *TenantStore) SubscribeInvalidation(ctx context.Context) {
	if s.l2 == nil {
		return
	}
	s.l2.Subscribe(ctx, "cache:invalidate", func(payload string) {
		// Only invalidate if this key is a tenant key.
		// (License invalidations will also arrive on this channel.)
		if len(payload) >= len(redisPrefix+"tenant:") && payload[:len(redisPrefix+"tenant:")] == redisPrefix+"tenant:" {
			s.l1.Invalidate(ctx, "", payload)
		}
	})
}
