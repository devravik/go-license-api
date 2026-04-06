package domain

import (
	"context"
	"time"
)

// LicenseRepository provides persistent license storage operations.
type LicenseRepository interface {
	FindByKey(ctx context.Context, tenantID, key string) (*License, error)
	Create(ctx context.Context, l *License) error
	Revoke(ctx context.Context, tenantID, key string) error
	// GetRecent returns a bounded list of recently updated licenses for warm-up.
	GetRecent(ctx context.Context, limit int) ([]License, error)
	// Update modifies persisted license fields.
	Update(ctx context.Context, l *License) error
	// ListByTenant returns a bounded page of licenses for a tenant.
	ListByTenant(ctx context.Context, tenantID string, limit, offset int) ([]*License, error)
	// Note: last_validated_at updates and similar admin-safe ops are optional and can
	// be provided by concrete repos without being part of the interface.
	// ListRevocationsSince returns revoked licenses since timestamp (or all if since is nil).
	ListRevocationsSince(ctx context.Context, since *time.Time, limit int) ([]Revocation, error)
}

// TenantRepository provides persistent tenant storage operations.
type TenantRepository interface {
	FindByID(ctx context.Context, id string) (*Tenant, error)
	FindByAPIKey(ctx context.Context, apiKey string) (*Tenant, error)
	FindAll(ctx context.Context) ([]*Tenant, error)
	Create(ctx context.Context, t *Tenant) error
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateLimits(ctx context.Context, id string, rps, burst int) error
	UpdateIPAllowlist(ctx context.Context, id string, cidrs []string) error
	RotateAPIKey(ctx context.Context, id, newKey string, gracePeriod time.Duration) error
}

// ActivationRepository handles seat tracking.
type ActivationRepository interface {
	ActivateWithLock(ctx context.Context, tenantID, key string, record *ActivationRecord) (remaining int, err error)
	Release(ctx context.Context, activationID string) error
	ReleaseByClient(ctx context.Context, tenantID, key, clientID string) error
	FindActiveByClient(ctx context.Context, tenantID, key, clientID string) (*ActivationRecord, error)
	CountActive(ctx context.Context, licenseID string) (int, error)
	RecordUsage(ctx context.Context, licenseID string, units int) (totalUsed int, limit *int, err error)
}

// AuditWriter appends audit entries; never reads.
type AuditWriter interface {
	Write(ctx context.Context, entry *AuditEntry)
	Flush()
}

// WebhookDispatcher fires events asynchronously.
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, event string, tenantID string, data any)
}

// ProductRepository provides persistent product storage operations (control plane).
// Data plane must never query DB; products are expected to be cached in-memory.
type ProductRepository interface {
	FindByID(ctx context.Context, tenantID, productID string) (*Product, error)
	FindByCode(ctx context.Context, tenantID, code string) (*Product, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*Product, error)
	ListUpdatedAfter(ctx context.Context, tenantID string, after time.Time) ([]*Product, error)
	Upsert(ctx context.Context, p *Product) error
	SetActive(ctx context.Context, tenantID, productID string, isActive bool) error
	Delete(ctx context.Context, tenantID, productID string) error
	Restore(ctx context.Context, tenantID, productID string) error
}

type PlanRepository interface {
	FindByID(ctx context.Context, tenantID, planID string) (*Plan, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*Plan, error)
	Upsert(ctx context.Context, p *Plan) error
	SetActive(ctx context.Context, tenantID, planID string, isActive bool) error
	Delete(ctx context.Context, tenantID, planID string) error
	Restore(ctx context.Context, tenantID, planID string) error
}
