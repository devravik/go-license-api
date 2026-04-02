package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

func TestLicenseStore_L1Hit(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}

	store := NewLicenseStore(l1, nil,
		1*time.Hour, 0,
		30*time.Minute, 15*time.Minute,
	)

	tenantID, key := "t1", "k1"
	lic := &domain.License{TenantID: tenantID, Key: key, Status: "active", Product: "pro", Plan: "starter"}
	store.Set(ctx, tenantID, key, lic)

	got, err := store.Get(ctx, tenantID, key)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != lic {
		t.Fatalf("expected same license pointer from cache")
	}

	// Ensure entry isn't marked negative.
	ce, ok := l1.Get(ctx, cacheKey(tenantID, key))
	if !ok {
		t.Fatalf("expected cache entry to exist")
	}
	if ce.Negative {
		t.Fatalf("expected positive cache entry")
	}
}

func TestLicenseStore_FullMissNegativeCaches(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}

	store := NewLicenseStore(l1, nil,
		1*time.Hour, 0,
		30*time.Minute, 15*time.Minute,
	)

	tenantID, key := "t1", "missing"

	got, err := store.Get(ctx, tenantID, key)
	if err == nil || err != domain.ErrLicenseNotFound {
		t.Fatalf("expected ErrLicenseNotFound, got lic=%v err=%v", got, err)
	}
	if got != nil {
		t.Fatalf("expected nil license on miss")
	}

	// Full miss should set a negative entry (bounded).
	ce, ok := l1.Get(ctx, cacheKey(tenantID, key))
	if !ok {
		t.Fatalf("expected negative cache entry to be set")
	}
	if !ce.Negative {
		t.Fatalf("expected negative cache entry")
	}

	// Second Get should hit L1 negative without changing the semantic result.
	got2, err2 := store.Get(ctx, tenantID, key)
	if err2 == nil || err2 != domain.ErrLicenseNotFound {
		t.Fatalf("expected ErrLicenseNotFound on second read, got lic=%v err=%v", got2, err2)
	}
	if got2 != nil {
		t.Fatalf("expected nil license on negative L1 hit")
	}
}

func TestLicenseStore_WriteThroughOverwrites(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}
	store := NewLicenseStore(l1, nil,
		1*time.Hour, 0,
		30*time.Minute, 15*time.Minute,
	)

	tenantID, key := "t1", "k1"
	old := &domain.License{TenantID: tenantID, Key: key, Status: "active", Product: "pro", Plan: "starter"}
	newLic := &domain.License{TenantID: tenantID, Key: key, Status: "revoked", Product: "pro", Plan: "starter"}

	store.Set(ctx, tenantID, key, old)
	got1, err := store.Get(ctx, tenantID, key)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got1 != old {
		t.Fatalf("expected old license from cache")
	}

	// Admin update should overwrite the cached object for the same (tenant,key).
	store.Set(ctx, tenantID, key, newLic)
	got2, err := store.Get(ctx, tenantID, key)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got2 != newLic {
		t.Fatalf("expected updated license from cache")
	}
}

func TestLicenseStore_ConcurrentReadWrite(t *testing.T) {
	ctx := context.Background()
	l1, err := NewL1Cache(10)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}
	store := NewLicenseStore(l1, nil,
		1*time.Hour, 0,
		30*time.Minute, 15*time.Minute,
	)

	tenantID, key := "t1", "k1"
	old := &domain.License{TenantID: tenantID, Key: key, Status: "active", Product: "pro", Plan: "starter"}
	newLic := &domain.License{TenantID: tenantID, Key: key, Status: "revoked", Product: "pro", Plan: "starter"}
	store.Set(ctx, tenantID, key, old)

	const readers = 16
	const iterations = 200

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(readers)

	readErr := make(chan error, readers*iterations)

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				got, err := store.Get(ctx, tenantID, key)
				if err != nil {
					readErr <- err
					return
				}
				if got != old && got != newLic {
					readErr <- fmt.Errorf("unexpected license pointer (got=%p old=%p new=%p)", got, old, newLic)
					return
				}
			}
		}()
	}

	close(start)
	// Concurrently mutate the same cache key.
	store.Set(ctx, tenantID, key, newLic)

	wg.Wait()
	close(readErr)
	for err := range readErr {
		if err != nil {
			t.Fatalf("concurrent read/write failure: %v", err)
		}
	}
}

