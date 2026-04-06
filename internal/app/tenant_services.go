package app

import "context"

// TenantAPIKeyService validates tenant API keys and resolves tenant identity.
type TenantAPIKeyService interface {
	// Validate returns the tenantID for a given apiKey and remoteIP if valid.
	// Implementations MUST use cache-only lookups in hot path.
	Validate(ctx context.Context, apiKey string, remoteIP string) (string, error)
}

// TenantLicenseAdminService defines tenant-scoped license operations.
// Implementations MUST write-through cache updates or invalidations.
type TenantLicenseAdminService interface {
	Create(ctx context.Context, tenantID string, in any) (string, error)
	Update(ctx context.Context, tenantID, licenseKey string, in any) error
	Revoke(ctx context.Context, tenantID, licenseKey string) error
	Get(ctx context.Context, tenantID, licenseKey string) (any, error)
	List(ctx context.Context, tenantID string, filter any) (any, error)
}

// TenantProductAdminService defines tenant-scoped product operations.
type TenantProductAdminService interface {
	Upsert(ctx context.Context, tenantID string, in any) (string, error)
	Delete(ctx context.Context, tenantID, productID string) error
	Get(ctx context.Context, tenantID, productID string) (any, error)
	List(ctx context.Context, tenantID string, filter any) (any, error)
}

