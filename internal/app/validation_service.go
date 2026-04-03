package app

import (
	"context"
	"errors"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/ports"
)

type ValidationService interface {
	Validate(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error)
}

type validationService struct {
	tenants             ports.TenantStore
	licenses            ports.LicenseStore
	repo                domain.LicenseRepository
	cacheWriter         ports.LicenseCacheWriter
	auditCh             chan<- *domain.AuditEntry
	minLicenseKeyLength int
}

func NewValidationService(tenants ports.TenantStore, licenses ports.LicenseStore, repo domain.LicenseRepository, cacheWriter ports.LicenseCacheWriter, auditCh chan<- *domain.AuditEntry, minLicenseKeyLength int) ValidationService {
	return &validationService{
		tenants:             tenants,
		licenses:            licenses,
		repo:                repo,
		cacheWriter:         cacheWriter,
		auditCh:             auditCh,
		minLicenseKeyLength: minLicenseKeyLength,
	}
}

func (s *validationService) Validate(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
	if tenantID == "" || apiKey == "" || key == "" {
		s.auditFailure(ctx, tenantID, key, "invalid_request")
		return &domain.ValidationResult{Valid: false, Error: "invalid_request"}, nil
	}
	if len(key) < s.minLicenseKeyLength {
		s.auditFailure(ctx, tenantID, key, "invalid_key")
		return &domain.ValidationResult{Valid: false, Error: "invalid_key"}, nil
	}

	tenant, err := s.tenants.Get(ctx, tenantID, apiKey)
	if err != nil {
		s.auditFailure(ctx, tenantID, key, "invalid_tenant")
		return &domain.ValidationResult{Valid: false, Error: "invalid_tenant"}, nil
	}
	if tenant.IsSuspended() {
		s.auditFailure(ctx, tenantID, key, "tenant_suspended")
		return &domain.ValidationResult{Valid: false, Error: "tenant_suspended"}, nil
	}

	lic, err := s.licenses.Get(ctx, tenant.ID, key)
	if err != nil {
		// Cache miss: attempt DB fallback, then write-through cache on success.
		if errors.Is(err, domain.ErrLicenseNotFound) && s.repo != nil {
			if dbLic, derr := s.repo.FindByKey(ctx, tenant.ID, key); derr == nil && dbLic != nil {
				lic = dbLic
				if s.cacheWriter != nil {
					s.cacheWriter.Set(ctx, tenant.ID, key, dbLic)
				}
			} else {
				s.auditFailure(ctx, tenantID, key, "license_not_found")
				return &domain.ValidationResult{Valid: false, Error: "license_not_found"}, nil
			}
		} else {
			s.auditFailure(ctx, tenantID, key, "license_lookup_error")
			return &domain.ValidationResult{Valid: false, Error: "license_not_found"}, nil
		}
	}

	// Minimal validation rules (domain-first)
	if lic.IsRevoked() {
		s.auditOutcome(ctx, tenantID, key, "failure", domain.EventLicenseFailed)
		return &domain.ValidationResult{Valid: false, Error: "license_revoked"}, nil
	}
	if product != "" && lic.Product != product {
		s.auditFailure(ctx, tenantID, key, "invalid_product")
		return &domain.ValidationResult{Valid: false, Error: "invalid_product"}, nil
	}
	gracePeriodEndsAt := lic.GracePeriodEndsAt()
	inGrace := lic.IsInGracePeriod()
	expired := lic.IsExpired()
	if expired && !inGrace {
		s.auditOutcome(ctx, tenantID, key, "failure", domain.EventLicenseFailed)
		return &domain.ValidationResult{Valid: false, Error: "license_expired"}, nil
	}
	s.auditOutcome(ctx, tenantID, key, "success", domain.EventLicenseValidated)

	return &domain.ValidationResult{
		Valid: true,
		Meta: &domain.ValidationMeta{
			Plan:              lic.Plan,
			Product:           lic.Product,
			ExpiresAt:         lic.ExpiresAt,
			SeatsTotal:        lic.SeatCount,
			Trial:             lic.IsTrial,
			GracePeriodEndsAt: gracePeriodEndsAt,
			Features:          lic.Features,
			Version:           lic.Version,
			InGracePeriod:     inGrace,
		},
	}, nil
}

func (s *validationService) auditOutcome(_ context.Context, tenantID, key, outcome, event string) {
	if s.auditCh == nil {
		return
	}
	entry := &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      event,
		ResourceID: key,
		Outcome:    outcome,
	}
	select {
	case s.auditCh <- entry:
	default:
	}
}

func (s *validationService) auditFailure(_ context.Context, tenantID, key, reason string) {
	if s.auditCh == nil {
		return
	}
	entry := &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventLicenseFailed,
		ResourceID: key,
		Outcome:    "failure",
		Meta:       map[string]any{"reason": reason},
		Severity:   "warning",
	}
	select {
	case s.auditCh <- entry:
	default:
	}
}
