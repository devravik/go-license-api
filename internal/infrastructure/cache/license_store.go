package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

// LicenseStore is cache-only on Get(); it never queries PostgreSQL.
// DB reads/writes happen in control plane and are pushed into the store via Set()/WarmUp().
type LicenseStore struct {
	l1 *L1Cache
	l2 *L2Cache // optional

	ttlL1       time.Duration
	ttlL2       time.Duration
	ttlActive   time.Duration
	ttlNegative time.Duration
}

func NewLicenseStore(l1 *L1Cache, l2 *L2Cache, ttlL1, ttlL2, ttlActive, ttlNegative time.Duration) *LicenseStore {
	return &LicenseStore{
		l1:          l1,
		l2:          l2,
		ttlL1:       ttlL1,
		ttlL2:       ttlL2,
		ttlActive:   ttlActive,
		ttlNegative: ttlNegative,
	}
}

func (s *LicenseStore) HasL2() bool { return s.l2 != nil }

// Get performs cache-only validation lookup.
func (s *LicenseStore) Get(ctx context.Context, tenantID, key string) (*domain.License, error) {
	ck := cacheKey(tenantID, key)

	// L1
	if entry, ok := s.l1.Get(ctx, ck); ok {
		if entry.Negative {
			return nil, domain.ErrLicenseNotFound
		}
		lic, ok := entry.Value.(*domain.License)
		if !ok || lic == nil {
			return nil, domain.ErrLicenseNotFound
		}
		// Extend L1 only (no Redis writes on hot path)
		if time.Until(entry.ExpiresAt) < s.ttlActive/2 {
			s.l1.Set(ctx, ck, &CacheEntry{Value: lic}, s.ttlActive)
		}
		return lic, nil
	}

	// L2
	if s.l2 != nil {
		if entry, ok := s.l2.Get(ctx, ck); ok {
			if entry.Negative {
				// Backfill negative to L1 briefly (L2 negative must not be refreshed on read).
				s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
				return nil, domain.ErrLicenseNotFound
			}
			lic, ok := entry.Value.(*domain.License)
			if !ok || lic == nil {
				return nil, domain.ErrLicenseNotFound
			}
			// Backfill L1 with L1 TTL
			s.l1.Set(ctx, ck, &CacheEntry{Value: lic}, s.ttlL1)
			return lic, nil
		}
	}

	// Full miss: cache-only invalid. Negative cache is L1-only and bounded.
	if s.l1.Len() < (s.l1.MaxEntries()*9)/10 {
		s.l1.Set(ctx, ck, &CacheEntry{Negative: true}, s.ttlNegative)
	}
	return nil, domain.ErrLicenseNotFound
}

// Set performs write-through into cache. MUST be called after any mutation.
func (s *LicenseStore) Set(ctx context.Context, tenantID, key string, license *domain.License) {
	ck := cacheKey(tenantID, key)
	s.l1.Set(ctx, ck, &CacheEntry{Value: license}, s.ttlL1)
	// Global index update (L1-only) using distinct namespace
	s.l1.Set(ctx, "gk:"+key, &CacheEntry{Value: license}, s.ttlL1)
	if s.l2 != nil {
		s.l2.Set(ctx, ck, &CacheEntry{Value: license}, s.ttlL2)
	}
}

// Invalidate removes a single license from cache and publishes invalidation for other instances.
func (s *LicenseStore) Invalidate(ctx context.Context, tenantID, key string) error {
	ck := cacheKey(tenantID, key)
	s.l1.Invalidate(ctx, "", ck)
	s.l1.Invalidate(ctx, "", "gk:"+key)
	if s.l2 != nil {
		s.l2.Invalidate(ctx, "", ck)
		s.l2.Publish(ctx, "cache:invalidate", ck)
	}
	return nil
}

// InvalidateTenant removes all cache entries for a tenant and propagates invalidation.
func (s *LicenseStore) InvalidateTenant(ctx context.Context, tenantID string) error {
	prefix := redisPrefix + tenantID + ":"
	s.l1.InvalidateAll(ctx, prefix)
	// Best-effort: key-only index cannot be efficiently scoped; rely on TTL.
	if s.l2 != nil {
		if err := s.l2.InvalidateAll(ctx, prefix); err != nil {
			return fmt.Errorf("L2 tenant invalidation partial: %w", err)
		}
		s.l2.Publish(ctx, "cache:invalidate:tenant", tenantID)
	}
	return nil
}

// SubscribeInvalidation wires Pub/Sub invalidation listeners (reconnecting) for multi-instance setups.
func (s *LicenseStore) SubscribeInvalidation(ctx context.Context) {
	if s.l2 == nil {
		return
	}
	s.l2.Subscribe(ctx, "cache:invalidate", func(payload string) {
		s.l1.Invalidate(ctx, "", payload)
	})
	s.l2.Subscribe(ctx, "cache:invalidate:tenant", func(tenantID string) {
		prefix := redisPrefix + tenantID + ":"
		s.l1.InvalidateAll(ctx, prefix)
	})
}

// WarmUp populates cache with a bounded set of recently updated licenses.
// This must be invoked from control-plane startup code (DB allowed there), never from validation.
func (s *LicenseStore) WarmUp(ctx context.Context, repo domain.LicenseRepository, limit int) error {
	licenses, err := repo.GetRecent(ctx, limit)
	if err != nil {
		return err
	}
	for i := range licenses {
		lic := licenses[i]
		// Only warm up active, non-deleted licenses.
		if lic.Status != "active" || lic.DeletedAt != nil {
			continue
		}
		ck := cacheKey(lic.TenantID, lic.Key)
		if _, ok := s.l1.Get(ctx, ck); ok {
			continue // don't overwrite hot entries unnecessarily
		}
		s.Set(ctx, lic.TenantID, lic.Key, &lic)
	}
	return nil
}

// GetByGlobalKey provides a license-key-only lookup for the public data plane.
// It is cache-only and uses the L1 key index. Full miss => ErrLicenseNotFound.
func (s *LicenseStore) GetByGlobalKey(ctx context.Context, key string) (*domain.License, error) {
	if entry, ok := s.l1.Get(ctx, "gk:"+key); ok {
		if entry.Negative {
			return nil, domain.ErrLicenseNotFound
		}
		if lic, ok := entry.Value.(*domain.License); ok && lic != nil {
			return lic, nil
		}
	}
	// Do not consult L2 or DB here; public path must be cache-only.
	// Negative-cache briefly to avoid hammering.
	if s.l1.Len() < (s.l1.MaxEntries()*9)/10 {
		s.l1.Set(ctx, "gk:"+key, &CacheEntry{Negative: true}, s.ttlNegative)
	}
	return nil, domain.ErrLicenseNotFound
}
