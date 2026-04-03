package ports

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

// LicenseCacheWriter allows write-through of licenses into cache from validation fallback path.
type LicenseCacheWriter interface {
	Set(ctx context.Context, tenantID, key string, license *domain.License)
}

