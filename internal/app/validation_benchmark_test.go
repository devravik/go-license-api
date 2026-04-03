package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
)

type benchTenantStore struct {
	tenant *domain.Tenant
}

func (s *benchTenantStore) Get(_ context.Context, _, _ string) (*domain.Tenant, error) {
	return s.tenant, nil
}

type benchLicenseStore struct {
	license *domain.License
}

func (s *benchLicenseStore) Get(_ context.Context, _, _ string) (*domain.License, error) {
	return s.license, nil
}

func BenchmarkValidation(b *testing.B) {
	expiresAt := time.Now().Add(24 * time.Hour)
	tenant := &domain.Tenant{ID: "t1", APIKey: "tenant-key", Status: "active"}
	license := &domain.License{
		TenantID:  "t1",
		Key:       "LIC-1",
		Product:   "pro",
		Plan:      "starter",
		Status:    "active",
		ExpiresAt: &expiresAt,
		Features:  []string{"sso"},
	}

	svc := app.NewValidationService(
		&benchTenantStore{tenant: tenant},
		&benchLicenseStore{license: license},
		nil,
		nil,
		nil,
		4,
	)

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = svc.Validate(ctx, "t1", "tenant-key", "LIC-1", "pro")
	}
}
