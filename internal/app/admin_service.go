package app

import (
	"context"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

type AdminService interface {
	RevokeLicense(ctx context.Context, tenantID, key string) error
	SuspendTenant(ctx context.Context, tenantID, reason string) error
	RotateTenantAPIKey(ctx context.Context, tenantID, newKey string, gracePeriod time.Duration) error
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

type RateLimiterCache interface {
	Invalidate(tenantID string)
}

type adminService struct {
	licenses domain.LicenseRepository
	tenants  domain.TenantRepository
	licCache LicenseCache
	tenCache TenantCache
	limiter  RateLimiterCache
}

func NewAdminService(licenses domain.LicenseRepository, tenants domain.TenantRepository, licCache LicenseCache, tenCache TenantCache, limiter RateLimiterCache) AdminService {
	return &adminService{licenses: licenses, tenants: tenants, licCache: licCache, tenCache: tenCache, limiter: limiter}
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
	return nil
}

func (s *adminService) RotateTenantAPIKey(ctx context.Context, tenantID, newKey string, gracePeriod time.Duration) error {
	if err := s.tenants.RotateAPIKey(ctx, tenantID, newKey, gracePeriod); err != nil {
		return fmt.Errorf("rotate api key: %w", err)
	}
	t, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("fetch tenant after rotate: %w", err)
	}
	// Post-commit consistency: clear stale keys then write-through current keys.
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.tenCache.Set(ctx, tenantID, t.APIKey, t)
	if t.OldAPIKey != "" {
		s.tenCache.Set(ctx, tenantID, t.OldAPIKey, t)
	}
	s.limiter.Invalidate(tenantID)
	return nil
}
