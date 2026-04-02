package app

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

type ValidationService interface {
	Validate(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error)
}

type validationService struct {
	tenants  TenantStore
	licenses LicenseStore
}

type TenantStore interface {
	Get(ctx context.Context, tenantID, apiKey string) (*domain.Tenant, error)
}

type LicenseStore interface {
	Get(ctx context.Context, tenantID, key string) (*domain.License, error)
}

func NewValidationService(tenants TenantStore, licenses LicenseStore) ValidationService {
	return &validationService{tenants: tenants, licenses: licenses}
}

func (s *validationService) Validate(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
	if tenantID == "" || apiKey == "" || key == "" {
		return &domain.ValidationResult{Valid: false, Error: "invalid_request"}, nil
	}

	tenant, err := s.tenants.Get(ctx, tenantID, apiKey)
	if err != nil {
		return &domain.ValidationResult{Valid: false, Error: "invalid_tenant"}, nil
	}
	if tenant.IsSuspended() {
		return &domain.ValidationResult{Valid: false, Error: "tenant_suspended"}, nil
	}

	lic, err := s.licenses.Get(ctx, tenant.ID, key)
	if err != nil {
		return &domain.ValidationResult{Valid: false, Error: "license_not_found"}, nil
	}

	// Minimal validation rules (domain-first)
	if lic.IsRevoked() {
		return &domain.ValidationResult{Valid: false, Error: "license_revoked"}, nil
	}
	if lic.IsExpired() {
		if lic.IsInGracePeriod() {
			return &domain.ValidationResult{
				Valid:             false,
				Error:             "license_grace_period",
				GracePeriodEndsAt: lic.GracePeriodEndsAt(),
			}, nil
		}
		return &domain.ValidationResult{Valid: false, Error: "license_expired"}, nil
	}
	if product != "" && lic.Product != "" && lic.Product != product {
		return &domain.ValidationResult{Valid: false, Error: "product_mismatch"}, nil
	}

	return &domain.ValidationResult{
		Valid: true,
		Meta: map[string]any{
			"tenant_id": tenant.ID,
			"product":   lic.Product,
			"plan":      lic.Plan,
		},
	}, nil
}
