package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

type testTenantStore struct {
	tenant *domain.Tenant
	err    error
}

func (s *testTenantStore) Get(_ context.Context, _, _ string) (*domain.Tenant, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tenant, nil
}

type testLicenseStore struct {
	byTenant map[string]*domain.License
	err      error
}

func (s *testLicenseStore) Get(_ context.Context, tenantID, _ string) (*domain.License, error) {
	if s.err != nil {
		return nil, s.err
	}
	lic, ok := s.byTenant[tenantID]
	if !ok {
		return nil, errors.New("license_not_found")
	}
	return lic, nil
}

func activeTenant() *domain.Tenant {
	return &domain.Tenant{ID: "t1", APIKey: "tenant-key", Status: "active"}
}

func TestValidationService_LicenseNotFound(t *testing.T) {
	svc := NewValidationService(
		&testTenantStore{tenant: activeTenant()},
		&testLicenseStore{byTenant: map[string]*domain.License{}},
		nil,
		nil,
		nil,
		8,
	)

	got, err := svc.Validate(context.Background(), "t1", "tenant-key", "abc-12345", "pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Valid || got.Error != "license_not_found" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestValidationService_RevokedLicense(t *testing.T) {
	lic := &domain.License{TenantID: "t1", Status: "revoked", Product: "pro"}
	svc := NewValidationService(
		&testTenantStore{tenant: activeTenant()},
		&testLicenseStore{byTenant: map[string]*domain.License{"t1": lic}},
		nil,
		nil,
		nil,
		8,
	)

	got, err := svc.Validate(context.Background(), "t1", "tenant-key", "abc-12345", "pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Valid || got.Error != "license_revoked" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestValidationService_ExpiredLicense(t *testing.T) {
	expired := time.Now().Add(-48 * time.Hour)
	lic := &domain.License{
		TenantID:        "t1",
		Status:          "active",
		Product:         "pro",
		ExpiresAt:       &expired,
		GracePeriodDays: 1,
	}
	svc := NewValidationService(
		&testTenantStore{tenant: activeTenant()},
		&testLicenseStore{byTenant: map[string]*domain.License{"t1": lic}},
		nil,
		nil,
		nil,
		8,
	)

	got, err := svc.Validate(context.Background(), "t1", "tenant-key", "abc-12345", "pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Valid || got.Error != "license_expired" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestValidationService_GracePeriodLicense(t *testing.T) {
	inGrace := time.Now().Add(-2 * time.Hour)
	lic := &domain.License{
		TenantID:        "t1",
		Status:          "active",
		Product:         "pro",
		ExpiresAt:       &inGrace,
		GracePeriodDays: 1,
	}
	svc := NewValidationService(
		&testTenantStore{tenant: activeTenant()},
		&testLicenseStore{byTenant: map[string]*domain.License{"t1": lic}},
		nil,
		nil,
		nil,
		8,
	)

	got, err := svc.Validate(context.Background(), "t1", "tenant-key", "abc-12345", "pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Valid || got.Meta == nil || !got.Meta.InGracePeriod {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestValidationService_ValidLicense(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour)
	seats := 10
	lic := &domain.License{
		TenantID:  "t1",
		Status:    "active",
		Product:   "pro",
		Plan:      "pro",
		IsTrial:   false,
		ExpiresAt: &expiresAt,
		SeatCount: &seats,
		Features:  []string{"sso"},
	}
	svc := NewValidationService(
		&testTenantStore{tenant: activeTenant()},
		&testLicenseStore{byTenant: map[string]*domain.License{"t1": lic}},
		nil,
		nil,
		nil,
		8,
	)

	got, err := svc.Validate(context.Background(), "t1", "tenant-key", "abc-12345", "pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Valid || got.Meta == nil || got.Meta.Plan == nil || got.Meta.Plan.ID != "pro" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestValidationService_WrongProduct(t *testing.T) {
	lic := &domain.License{TenantID: "t1", Status: "active", Product: "pro"}
	svc := NewValidationService(
		&testTenantStore{tenant: activeTenant()},
		&testLicenseStore{byTenant: map[string]*domain.License{"t1": lic}},
		nil,
		nil,
		nil,
		8,
	)

	got, err := svc.Validate(context.Background(), "t1", "tenant-key", "abc-12345", "enterprise")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Valid || got.Error != "invalid_product" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestValidationService_TenantIsolation(t *testing.T) {
	svc := NewValidationService(
		&testTenantStore{tenant: &domain.Tenant{ID: "t2", APIKey: "tenant-key", Status: "active"}},
		&testLicenseStore{byTenant: map[string]*domain.License{
			"t1": {TenantID: "t1", Status: "active", Product: "pro"},
		}},
		nil,
		nil,
		nil,
		8,
	)

	got, err := svc.Validate(context.Background(), "t2", "tenant-key", "abc-12345", "pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Valid || got.Error != "license_not_found" {
		t.Fatalf("unexpected result: %+v", got)
	}
}
