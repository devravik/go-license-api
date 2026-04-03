package loadtest

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

type SeedConfig struct {
	Tenants  int
	Products int
	Licenses int
}

type SeedDeps interface {
	CreateTenant(ctx context.Context, rps, burst int) (*domain.Tenant, string, error)
	UpdateTenantProfile(ctx context.Context, tenantID, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	UpsertProduct(ctx context.Context, p *domain.Product) error
	CreateLicense(ctx context.Context, l *domain.License) error
	WriteThroughLicense(ctx context.Context, tenantID, key string, lic *domain.License)
	// Events (optional; no-op when not provided)
	PublishTenantCreated(ctx context.Context, tenantID string)
	PublishTenantUpdated(ctx context.Context, tenantID string)
	PublishProductUpserted(ctx context.Context, tenantID, code string)
}

type SeedArtifacts struct {
	Tenants          []TenantInfo
	LicensesByTenant map[string][]LicenseInfo
}

func Seed(ctx context.Context, cfg SeedConfig, deps SeedDeps) (*SeedArtifacts, error) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	arts := &SeedArtifacts{
		Tenants:          make([]TenantInfo, 0, cfg.Tenants),
		LicensesByTenant: make(map[string][]LicenseInfo, cfg.Tenants),
	}
	for i := 0; i < cfg.Tenants; i++ {
		tenant, apiKey, err := deps.CreateTenant(ctx, 100, 200)
		if err != nil {
			return nil, fmt.Errorf("create tenant: %w", err)
		}
		// Immediately set unique slug/name to satisfy unique constraint before creating next tenant.
		name := fmt.Sprintf("Tenant %d", i+1)
		slug := fmt.Sprintf("t-%d-%d", time.Now().UnixNano(), rng.Intn(1_000_000))
		_ = deps.UpdateTenantProfile(ctx, tenant.ID, name, slug, "", "", "", 0, nil)
		// Notify other processes (server) to backfill cache
		deps.PublishTenantCreated(ctx, tenant.ID)
		ti := TenantInfo{ID: tenant.ID, APIKey: apiKey}
		arts.Tenants = append(arts.Tenants, ti)

		// products
		for p := 0; p < cfg.Products; p++ {
			prod := &domain.Product{
				ID:       randomUUID(rng),
				TenantID: tenant.ID,
				Code:     fmt.Sprintf("prod_%d", p+1),
				Name:     fmt.Sprintf("Product %d", p+1),
				IsActive: true,
			}
			if err := deps.UpsertProduct(ctx, prod); err != nil {
				return nil, fmt.Errorf("upsert product: %w", err)
			}
			// Publish for cross-process cache refresh (if wired)
			deps.PublishProductUpserted(ctx, tenant.ID, prod.Code)
		}

		// licenses
		lics := make([]LicenseInfo, 0, cfg.Licenses)
		for k := 0; k < cfg.Licenses; k++ {
			key := randomKey(rng)
			// Seed-only licenses are always valid for testing:
			// - active status
			// - long expiry window
			exp := time.Now().Add(365 * 24 * time.Hour)
			lic := &domain.License{
				TenantID:  tenant.ID,
				Key:       key,
				Product:   "prod_1",
				Status:    "active",
				ExpiresAt: &exp,
			}
			if err := deps.CreateLicense(ctx, lic); err != nil {
				return nil, fmt.Errorf("create license: %w", err)
			}
			deps.WriteThroughLicense(ctx, tenant.ID, key, lic)
			lics = append(lics, LicenseInfo{Key: key})
		}
		arts.LicensesByTenant[tenant.ID] = lics
		// Mark tenant updated after bulk license creation (optional signal)
		deps.PublishTenantUpdated(ctx, tenant.ID)
	}
	return arts, nil
}

func randomUUID(rng *rand.Rand) string {
	// pseudo-uuid for seed products; not critical
	return fmt.Sprintf("id_%d_%d", time.Now().UnixNano(), rng.Int63())
}
