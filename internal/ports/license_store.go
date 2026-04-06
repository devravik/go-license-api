package ports

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

type LicenseStore interface {
	Get(ctx context.Context, tenantID, key string) (*domain.License, error)
}
