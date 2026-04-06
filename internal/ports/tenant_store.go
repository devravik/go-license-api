package ports

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

type TenantStore interface {
	Get(ctx context.Context, tenantID, apiKey string) (*domain.Tenant, error)
}
