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
}

// TenantRepository provides persistent tenant storage operations.
type TenantRepository interface {
	FindByID(ctx context.Context, id string) (*Tenant, error)
	FindByAPIKey(ctx context.Context, apiKey string) (*Tenant, error)
	FindAll(ctx context.Context) ([]*Tenant, error)
	Create(ctx context.Context, t *Tenant) error
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateIPAllowlist(ctx context.Context, id string, cidrs []string) error
	RotateAPIKey(ctx context.Context, id, newKey string, gracePeriod time.Duration) error
}

// ActivationRepository handles seat tracking.
type ActivationRepository interface {
	ActivateWithLock(ctx context.Context, tenantID, key string, record *ActivationRecord) (remaining int, err error)
	Release(ctx context.Context, activationID string) error
	CountActive(ctx context.Context, licenseID int) (int, error)
	RecordUsage(ctx context.Context, licenseID, units int) error
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
}
