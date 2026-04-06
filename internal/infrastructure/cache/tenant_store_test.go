package cache

import (
	"context"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

func TestTenantStore_L1Hit(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}

	store := &TenantStore{
		l1:          l1,
		l2:          nil,
		ttl:         1 * time.Hour,
		ttlNegative: 15 * time.Minute,
	}

	tenantID, apiKey := "t1", "tenant-key"
	tenant := &domain.Tenant{ID: tenantID, APIKey: apiKey, Status: "active"}
	store.Set(ctx, tenantID, apiKey, tenant)

	got, err := store.Get(ctx, tenantID, apiKey)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != tenant {
		t.Fatalf("expected same tenant pointer from cache")
	}
}

func TestTenantStore_FullMissNegativeCaches(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}

	store := &TenantStore{
		l1:          l1,
		l2:          nil,
		ttl:         1 * time.Hour,
		ttlNegative: 15 * time.Minute,
	}

	tenantID, apiKey := "t1", "missing-key"

	got, err := store.Get(ctx, tenantID, apiKey)
	if err == nil || err != domain.ErrInvalidTenant {
		t.Fatalf("expected ErrInvalidTenant, got tenant=%v err=%v", got, err)
	}
	if got != nil {
		t.Fatalf("expected nil tenant on miss")
	}

	ce, ok := l1.Get(ctx, cacheKey(tenantID, apiKey))
	if !ok {
		t.Fatalf("expected negative cache entry to be set")
	}
	if !ce.Negative {
		t.Fatalf("expected negative cache entry")
	}
}

func TestTenantStore_WriteThroughOverwrites(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}

	store := &TenantStore{
		l1:          l1,
		l2:          nil,
		ttl:         1 * time.Hour,
		ttlNegative: 15 * time.Minute,
	}

	tenantID, apiKey := "t1", "tenant-key"
	old := &domain.Tenant{ID: tenantID, APIKey: apiKey, Status: "active"}
	newTenant := &domain.Tenant{ID: tenantID, APIKey: apiKey, Status: "suspended"}

	store.Set(ctx, tenantID, apiKey, old)
	got1, err := store.Get(ctx, tenantID, apiKey)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got1 != old {
		t.Fatalf("expected old tenant from cache")
	}

	store.Set(ctx, tenantID, apiKey, newTenant)
	got2, err := store.Get(ctx, tenantID, apiKey)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got2 != newTenant {
		t.Fatalf("expected updated tenant from cache")
	}
}

func TestTenantStore_OldKeyWithinGraceAccepted(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}

	store := &TenantStore{
		l1:          l1,
		l2:          nil,
		ttl:         1 * time.Hour,
		ttlNegative: 15 * time.Minute,
	}

	tenantID := "t1"
	currentKey := "tenant-key-current"
	oldKey := "tenant-key-old"
	expiresAt := time.Now().Add(30 * time.Minute)
	tenant := &domain.Tenant{
		ID:              tenantID,
		APIKey:          currentKey,
		OldAPIKey:       oldKey,
		OldKeyExpiresAt: &expiresAt,
		Status:          "active",
	}

	store.Set(ctx, tenantID, oldKey, tenant)
	got, err := store.Get(ctx, tenantID, oldKey)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != tenant {
		t.Fatalf("expected tenant for old key within grace window")
	}
}

func TestTenantStore_OldKeyExpiredRejectedAndInvalidated(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}

	store := &TenantStore{
		l1:          l1,
		l2:          nil,
		ttl:         1 * time.Hour,
		ttlNegative: 15 * time.Minute,
	}

	tenantID := "t1"
	currentKey := "tenant-key-current"
	oldKey := "tenant-key-old"
	expiredAt := time.Now().Add(-1 * time.Minute)
	tenant := &domain.Tenant{
		ID:              tenantID,
		APIKey:          currentKey,
		OldAPIKey:       oldKey,
		OldKeyExpiresAt: &expiredAt,
		Status:          "active",
	}

	store.Set(ctx, tenantID, oldKey, tenant)
	got, err := store.Get(ctx, tenantID, oldKey)
	if err == nil || err != domain.ErrInvalidTenant {
		t.Fatalf("expected ErrInvalidTenant for expired old key, got tenant=%v err=%v", got, err)
	}
	if got != nil {
		t.Fatalf("expected nil tenant for expired old key")
	}
	if _, ok := l1.Get(ctx, cacheKey(tenantID, oldKey)); ok {
		t.Fatalf("expected expired old key cache entry to be invalidated")
	}
}
