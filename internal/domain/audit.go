package domain

import "time"

// AuditEntry is an immutable event record. Written once, never updated.
type AuditEntry struct {
	ID string `json:"id"`

	TenantID string `json:"tenant_id"`

	ActorID string `json:"actor_id"` // API key ID or admin key ID.
	ActorIP string `json:"actor_ip"`

	Event      string `json:"event"`       // e.g. "license.validated".
	ResourceID string `json:"resource_id"` // license key, tenant ID, etc.
	Outcome    string `json:"outcome"`     // "success" | "failure".

	Meta map[string]any `json:"meta"`

	// Query-at-scale fields
	ResourceType string `json:"resource_type,omitempty"`
	Severity     string `json:"severity,omitempty"` // info | warning | error

	CreatedAt time.Time `json:"created_at"`
}

// Audit event constants — use these everywhere, never raw strings.
const (
	EventLicenseValidated   = "license.validated"
	EventLicenseFailed      = "license.validation_failed"
	EventLicenseActivated   = "license.activated"
	EventLicenseDeactivated = "license.deactivated"
	EventLicenseRevoked     = "license.revoked"
	EventTenantCreated      = "tenant.created"
	EventTenantSuspended    = "tenant.suspended"
	EventTenantReinstated   = "tenant.reinstated"
	EventTenantDeleted      = "tenant.deleted"
	EventTenantKeyRotated   = "tenant.key_rotated"
	EventTenantLimitsUpdated = "tenant.limits_updated"
	EventTenantIPAllowlistUpdated = "tenant.ip_allowlist_updated"
	EventAuthFailed         = "security.auth_failure"
	EventRateLimitBreach    = "security.rate_limit_breach"
	EventIPBlocked          = "security.ip_block"
)
