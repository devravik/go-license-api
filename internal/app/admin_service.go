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
	licenses domain.LicenseRepository
	tenants  domain.TenantRepository
	licCache LicenseCache
	tenCache TenantCache
	limiter  ports.RateLimiter
	auditor  domain.AuditWriter
}

func NewAdminService(licenses domain.LicenseRepository, tenants domain.TenantRepository, licCache LicenseCache, tenCache TenantCache, limiter ports.RateLimiter, auditor domain.AuditWriter) AdminService {
	return &adminService{licenses: licenses, tenants: tenants, licCache: licCache, tenCache: tenCache, limiter: limiter, auditor: auditor}
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

	tenant := &domain.Tenant{
		ID:     uuid.New().String(),
		APIKey: apiKey,
		RPS:    rps,
		Burst:  burst,
		Status: "active",
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
