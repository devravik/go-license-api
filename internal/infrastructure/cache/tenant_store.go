package cache

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

// TenantStore is cache-only on Get(); it never queries PostgreSQL.
// Tenant lookups in the validation path MUST use this store.
type TenantStore struct {
	l1 *L1Cache
	l2 *L2Cache // optional

	ttl         time.Duration
	ttlNegative time.Duration
}

func NewTenantStore(l1 *L1Cache, l2 *L2Cache, ttl, ttlNegative time.Duration) *TenantStore {
	s := &TenantStore{l1: l1, l2: l2, ttl: ttl, ttlNegative: ttlNegative}
	go s.cleanupLoop()
	return s
}

func (s *TenantStore) HasL2() bool { return s.l2 != nil }

func (s *TenantStore) cacheKey(tenantID, apiKey string) string {
	return cacheKey(tenantID, apiKey)
}

func (s *TenantStore) cacheKeyByAPIKey(apiKey string) string {
	return cacheKey("by_api_key", apiKey)
}

// Get is cache-only in validation path. Miss => invalid tenant.
func (s *TenantStore) Get(ctx context.Context, tenantID, apiKey string) (*domain.Tenant, error) {
	ck := s.cacheKey(tenantID, apiKey)

	if entry, ok := s.l1.Get(ctx, ck); ok {
		if entry.Negative {
			return nil, domain.ErrInvalidTenant
		}
		t, ok := entry.Value.(*domain.Tenant)
		if !ok || t == nil {
			return nil, domain.ErrInvalidTenant
		}
		if !t.AcceptsKey(apiKey) {
			// Rotation grace expired (or mismatched key); do not trust stale cache entry.
			s.Invalidate(ctx, tenantID, apiKey)
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
			if !t.AcceptsKey(apiKey) {
				s.Invalidate(ctx, tenantID, apiKey)
				s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
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

// GetByAPIKey resolves the tenant directly from api key without client-provided tenant_id.
func (s *TenantStore) GetByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error) {
	byKey := s.cacheKeyByAPIKey(apiKey)
	if entry, ok := s.l1.Get(ctx, byKey); ok {
		if entry.Negative {
			return nil, domain.ErrInvalidTenant
		}
		t, ok := entry.Value.(*domain.Tenant)
		if !ok || t == nil || !t.AcceptsKey(apiKey) {
			return nil, domain.ErrInvalidTenant
		}
		return t, nil
	}
	if s.l2 != nil {
		if entry, ok := s.l2.Get(ctx, byKey); ok {
			if entry.Negative {
				s.l1.Set(ctx, byKey, &CacheEntry{Negative: true}, s.ttlNegative)
				return nil, domain.ErrInvalidTenant
			}
			t, ok := entry.Value.(*domain.Tenant)
			if !ok || t == nil || !t.AcceptsKey(apiKey) {
				s.l1.Set(ctx, byKey, &CacheEntry{Negative: true}, s.ttlNegative)
				return nil, domain.ErrInvalidTenant
			}
			s.l1.Set(ctx, byKey, &CacheEntry{Value: t}, s.ttl)
			return t, nil
		}
	}
	if s.l1.Len() < (s.l1.MaxEntries()*9)/10 {
		s.l1.Set(ctx, byKey, &CacheEntry{Negative: true}, s.ttlNegative)
	}
	return nil, domain.ErrInvalidTenant
}

// Set performs write-through for tenant updates (API key rotation, status changes, etc).
func (s *TenantStore) Set(ctx context.Context, tenantID, apiKey string, tenant *domain.Tenant) {
	ck := s.cacheKey(tenantID, apiKey)
	s.l1.Set(ctx, ck, &CacheEntry{Value: tenant}, s.ttl)
	s.l1.Set(ctx, s.cacheKeyByAPIKey(apiKey), &CacheEntry{Value: tenant}, s.ttl)
	if s.l2 != nil {
		s.l2.Set(ctx, ck, &CacheEntry{Value: tenant}, s.ttl)
		s.l2.Set(ctx, s.cacheKeyByAPIKey(apiKey), &CacheEntry{Value: tenant}, s.ttl)
	}
}

func (s *TenantStore) Invalidate(ctx context.Context, tenantID, apiKey string) {
	ck := s.cacheKey(tenantID, apiKey)
	s.l1.Invalidate(ctx, "", ck)
	s.l1.Invalidate(ctx, "", s.cacheKeyByAPIKey(apiKey))
	if s.l2 != nil {
		s.l2.Invalidate(ctx, "", ck)
		s.l2.Invalidate(ctx, "", s.cacheKeyByAPIKey(apiKey))
		s.l2.Publish(ctx, "cache:invalidate", ck)
		s.l2.Publish(ctx, "cache:invalidate", s.cacheKeyByAPIKey(apiKey))
	}
}

func (s *TenantStore) InvalidateByTenantID(ctx context.Context, tenantID string) {
	prefix := redisPrefix + tenantID + ":"
	s.l1.InvalidateAll(ctx, prefix)
	if s.l2 != nil {
		s.l2.InvalidateAll(ctx, prefix)
		s.l2.Publish(ctx, "cache:invalidate", prefix)
	}
}

func (s *TenantStore) SubscribeInvalidation(ctx context.Context) {
	if s.l2 == nil {
		return
	}
	s.l2.Subscribe(ctx, "cache:invalidate", func(payload string) {
		if len(payload) >= len(redisPrefix) && payload[:len(redisPrefix)] == redisPrefix {
			// Prefix payload means tenant-wide invalidation.
			if payload[len(payload)-1] == ':' {
				s.l1.InvalidateAll(ctx, payload)
				return
			}
			s.l1.Invalidate(ctx, "", payload)
		}
	})
}

func (s *TenantStore) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.l1.CleanupExpired(1000)
	}
}
