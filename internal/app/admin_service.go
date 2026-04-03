package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/ports"
	"github.com/google/uuid"
)

type AdminService interface {
	CreateTenant(ctx context.Context, rps, burst int) (*domain.Tenant, string, error)
	RevokeLicense(ctx context.Context, tenantID, key string) error
	SuspendTenant(ctx context.Context, tenantID, reason string) error
	ReinstateTenant(ctx context.Context, tenantID string) error
	DeleteTenant(ctx context.Context, tenantID string) error
	RotateTenantAPIKey(ctx context.Context, tenantID string, gracePeriod time.Duration) (string, time.Time, error)
	UpdateTenantLimits(ctx context.Context, tenantID string, rps, burst int) error
	UpdateTenantIPAllowlist(ctx context.Context, tenantID string, cidrs []string) error
	// Product control-plane methods
	UpsertProduct(ctx context.Context, p *domain.Product) error
	DeleteProduct(ctx context.Context, tenantID, productID string) error
	SetProductActive(ctx context.Context, tenantID, productID string, active bool) error
}

type LicenseCache interface {
	Set(ctx context.Context, tenantID, key string, license *domain.License)
	InvalidateTenant(ctx context.Context, tenantID string) error
}

type TenantCache interface {
	Set(ctx context.Context, tenantID, apiKey string, tenant *domain.Tenant)
	Invalidate(ctx context.Context, tenantID, apiKey string)
	InvalidateByTenantID(ctx context.Context, tenantID string)
}

type adminService struct {
	licenses  domain.LicenseRepository
	tenants   domain.TenantRepository
	products  domain.ProductRepository
	licCache  LicenseCache
	tenCache  TenantCache
	prodCache ports.ProductStore
	limiter   ports.RateLimiter
	auditor   domain.AuditWriter
}

func NewAdminService(licenses domain.LicenseRepository, tenants domain.TenantRepository, products domain.ProductRepository, licCache LicenseCache, tenCache TenantCache, prodCache ports.ProductStore, limiter ports.RateLimiter, auditor domain.AuditWriter) AdminService {
	return &adminService{
		licenses:  licenses,
		tenants:   tenants,
		products:  products,
		licCache:  licCache,
		tenCache:  tenCache,
		prodCache: prodCache,
		limiter:   limiter,
		auditor:   auditor,
	}
}

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *adminService) CreateTenant(ctx context.Context, rps, burst int) (*domain.Tenant, string, error) {
	if rps <= 0 || burst <= 0 {
		return nil, "", errors.New("invalid_limits")
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}

	// Pre-DB: generate a collision-resistant slug and basic name to avoid unique slug violations.
	// We do not query DB to check uniqueness; we rely on UUID entropy.
	genID := uuid.New().String()
	slug := "t-" + genID
	name := "Tenant " + genID[:8]

	tenant := &domain.Tenant{
		ID:     uuid.New().String(),
		APIKey: apiKey,
		RPS:    rps,
		Burst:  burst,
		Status: "active",
		Name:   name,
		Slug:   slug,
	}
	if err := s.tenants.Create(ctx, tenant); err != nil {
		return nil, "", fmt.Errorf("create tenant: %w", err)
	}

	s.tenCache.Set(ctx, tenant.ID, tenant.APIKey, tenant)
	s.limiter.Invalidate(tenant.ID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenant.ID,
		Event:      domain.EventTenantCreated,
		ResourceID: tenant.ID,
		Outcome:    "success",
	})
	return tenant, apiKey, nil
}

func (s *adminService) RevokeLicense(ctx context.Context, tenantID, key string) error {
	if err := s.licenses.Revoke(ctx, tenantID, key); err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	lic, err := s.licenses.FindByKey(ctx, tenantID, key)
	if err != nil {
		return fmt.Errorf("fetch revoked license: %w", err)
	}
	s.licCache.Set(ctx, tenantID, key, lic)
	return nil
}

func (s *adminService) SuspendTenant(ctx context.Context, tenantID, reason string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, "suspended"); err != nil {
		return fmt.Errorf("suspend tenant: %w", err)
	}
	// Invalidate all licenses for tenant in cache layers.
	if err := s.licCache.InvalidateTenant(ctx, tenantID); err != nil {
		return err
	}
	// Invalidate tenant API keys from cache (best effort).
	t, err := s.tenants.FindByID(ctx, tenantID)
	if err == nil && t != nil {
		s.tenCache.InvalidateByTenantID(ctx, tenantID)
		s.tenCache.Invalidate(ctx, tenantID, t.APIKey)
		s.limiter.Invalidate(tenantID)
	}
	_ = reason // reserved for future audit logging
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantSuspended,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) ReinstateTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, "active"); err != nil {
		return fmt.Errorf("reinstate tenant: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantReinstated,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) DeleteTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, "deleted"); err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantDeleted,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) RotateTenantAPIKey(ctx context.Context, tenantID string, gracePeriod time.Duration) (string, time.Time, error) {
	newKey, err := generateAPIKey()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate api key: %w", err)
	}

	if err := s.tenants.RotateAPIKey(ctx, tenantID, newKey, gracePeriod); err != nil {
		return "", time.Time{}, fmt.Errorf("rotate api key: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantKeyRotated,
		ResourceID: tenantID,
		Outcome:    "success",
	})

	return newKey, time.Now().Add(gracePeriod), nil
}

func (s *adminService) UpdateTenantLimits(ctx context.Context, tenantID string, rps, burst int) error {
	if rps <= 0 || burst <= 0 {
		return errors.New("invalid_limits")
	}
	if err := s.tenants.UpdateLimits(ctx, tenantID, rps, burst); err != nil {
		return fmt.Errorf("update tenant limits: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantLimitsUpdated,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) UpdateTenantIPAllowlist(ctx context.Context, tenantID string, cidrs []string) error {
	if err := s.tenants.UpdateIPAllowlist(ctx, tenantID, cidrs); err != nil {
		return fmt.Errorf("update tenant ip allowlist: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantIPAllowlistUpdated,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

// UpsertProduct writes through to cache to ensure immediate runtime availability.
func (s *adminService) UpsertProduct(ctx context.Context, p *domain.Product) error {
	if p == nil || p.TenantID == "" || p.Code == "" {
		return errors.New("invalid_product")
	}
	if err := s.products.Upsert(ctx, p); err != nil {
		return fmt.Errorf("product upsert: %w", err)
	}
	if s.prodCache != nil {
		s.prodCache.Set(ctx, p.TenantID, p.Code, p)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   p.TenantID,
		Event:      "product_upserted",
		ResourceID: p.ID,
		Outcome:    "success",
	})
	return nil
}

// DeleteProduct invalidates from cache; DB is source of truth for persistence.
func (s *adminService) DeleteProduct(ctx context.Context, tenantID, productID string) error {
	if tenantID == "" || productID == "" {
		return errors.New("invalid_request")
	}
	// Need code for cache invalidation: fetch by ID (control plane can hit DB).
	p, err := s.products.FindByID(ctx, tenantID, productID)
	if err != nil {
		return fmt.Errorf("product find: %w", err)
	}
	if err := s.products.Delete(ctx, tenantID, productID); err != nil {
		return fmt.Errorf("product delete: %w", err)
	}
	if s.prodCache != nil {
		s.prodCache.Invalidate(ctx, tenantID, p.Code)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      "product_deleted",
		ResourceID: productID,
		Outcome:    "success",
	})
	return nil
}

// SetProductActive toggles active flag and updates cache entry.
func (s *adminService) SetProductActive(ctx context.Context, tenantID, productID string, active bool) error {
	if tenantID == "" || productID == "" {
		return errors.New("invalid_request")
	}
	// fetch for code and current fields
	p, err := s.products.FindByID(ctx, tenantID, productID)
	if err != nil {
		return fmt.Errorf("product find: %w", err)
	}
	if err := s.products.SetActive(ctx, tenantID, productID, active); err != nil {
		return fmt.Errorf("product set_active: %w", err)
	}
	// reflect new flag and write-through
	p.IsActive = active
	if s.prodCache != nil {
		s.prodCache.Set(ctx, tenantID, p.Code, p)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      "product_set_active",
		ResourceID: productID,
		Outcome:    "success",
	})
	return nil
}

// UpdateTenantProfile is intentionally not part of the AdminService interface to keep
// backward compatibility for existing mocks and tests. Handlers can detect support via
// type assertion on this method set.
func (s *adminService) UpdateTenantProfile(ctx context.Context, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error {
	type tenantProfileRepo interface {
		UpdateProfile(ctx context.Context, id string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	}
	type tenantFinder interface {
		FindByID(ctx context.Context, id string) (*domain.Tenant, error)
	}
	tr, ok := s.tenants.(tenantProfileRepo)
	if !ok {
		return errors.New("update_profile_not_supported")
	}
	if err := tr.UpdateProfile(ctx, tenantID, name, slug, email, company, plan, maxLicenses, metadata); err != nil {
		return fmt.Errorf("update tenant profile: %w", err)
	}
	if tf, ok := s.tenants.(tenantFinder); ok {
		if t, err := tf.FindByID(ctx, tenantID); err == nil && t != nil {
			s.tenCache.Set(ctx, t.ID, t.APIKey, t)
		}
	}
	s.limiter.Invalidate(tenantID)
	return nil
}
