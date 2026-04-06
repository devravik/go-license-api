package app

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/ports"
)

const validationClockSkew = 5 * time.Minute

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
		s.auditFailure(ctx, tenantID, key, "license_not_found")
		return &domain.ValidationResult{Valid: false, Error: "license_not_found"}, nil
	}

	// Minimal validation rules (domain-first)
	if lic.IsRevoked() {
		s.auditOutcome(ctx, tenantID, key, "failure", domain.EventLicenseFailed)
		return &domain.ValidationResult{Valid: false, Error: "license_revoked"}, nil
	}
	if product != "" {
		if lic.ProductID != nil && *lic.ProductID != "" && *lic.ProductID != product {
			s.auditFailure(ctx, tenantID, key, "invalid_product")
			return &domain.ValidationResult{Valid: false, Error: "invalid_product"}, nil
		}
		if lic.Product != "" && lic.Product != product {
			s.auditFailure(ctx, tenantID, key, "invalid_product")
			return &domain.ValidationResult{Valid: false, Error: "invalid_product"}, nil
		}
	}
	if lic.NotBefore != nil {
		// Allow small skew tolerance to reduce false negatives on offline/unsynced clocks.
		earliestValid := lic.NotBefore.Add(-validationClockSkew)
		if time.Now().Before(earliestValid) {
			s.auditOutcome(ctx, tenantID, key, "failure", domain.EventLicenseFailed)
			return &domain.ValidationResult{Valid: false, Error: "license_not_active"}, nil
		}
	}
	gracePeriodEndsAt := lic.GracePeriodEndsAt()
	inGrace := lic.IsInGracePeriod()
	expired := lic.IsExpired()
	if expired && !inGrace {
		s.auditOutcome(ctx, tenantID, key, "failure", domain.EventLicenseFailed)
		return &domain.ValidationResult{Valid: false, Error: "license_expired"}, nil
	}
	s.auditOutcome(ctx, tenantID, key, "success", domain.EventLicenseValidated)
	features := lic.FinalFeatures
	if len(features) == 0 {
		features = lic.Features
	}
	seats := lic.SeatsTotal
	var planRef *domain.ValidationRef
	planID := ""
	if lic.PlanID != nil && *lic.PlanID != "" {
		planID = *lic.PlanID
		planRef = &domain.ValidationRef{ID: *lic.PlanID}
	} else if lic.Plan != "" {
		planID = lic.Plan
		planRef = &domain.ValidationRef{ID: lic.Plan, Name: lic.Plan}
	}
	var productRef *domain.ValidationRef
	productID := ""
	if lic.ProductID != nil {
		productID = *lic.ProductID
		productRef = &domain.ValidationRef{ID: *lic.ProductID}
	} else if lic.Product != "" {
		productID = lic.Product
		productRef = &domain.ValidationRef{ID: lic.Product, Name: lic.Product}
	}
	return &domain.ValidationResult{
		Valid: true,
		Meta: &domain.ValidationMeta{
			LicenseID:         lic.ID,
			Status:            lic.Status,
			Type:              lic.Type,
			PlanID:            planID,
			Plan:              planRef,
			Product:           productRef,
			ProductID:         productID,
			NotBefore:         lic.NotBefore,
			ExpiresAt:         lic.ExpiresAt,
			SeatsTotal:        &seats,
			UnlimitedSeats:    seats == -1,
			Trial:             lic.Trial.Enabled,
			GracePeriodEndsAt: gracePeriodEndsAt,
			Features:          features,
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
