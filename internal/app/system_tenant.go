package app

import (
	"context"
	"fmt"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/infrastructure/idgen"
	security "github.com/devravik/go-license-api/internal/security"
)

// EnsureSystemTenant guarantees there is at least one tenant by creating a
// system tenant on empty storage. IDs follow global NanoID prefix rules.
func EnsureSystemTenant(ctx context.Context, tenants domain.TenantRepository) error {
	existing, err := tenants.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return fmt.Errorf("generate system tenant api key: %w", err)
	}
	tenantID, err := idgen.NewID("ten")
	if err != nil {
		return fmt.Errorf("generate system tenant id: %w", err)
	}
	t := &domain.Tenant{
		ID:     tenantID,
		APIKey: security.HashAPIKey(apiKey),
		RPS:    100,
		Burst:  200,
		Status: "active",
		Name:   "System Tenant",
		Slug:   "system-tenant",
		Metadata: map[string]any{
			"is_system": true,
		},
	}
	if err := tenants.Create(ctx, t); err != nil {
		return fmt.Errorf("create system tenant: %w", err)
	}
	return nil
}
