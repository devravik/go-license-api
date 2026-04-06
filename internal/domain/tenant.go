package domain

import "time"

// Tenant represents a registered API consumer.
type Tenant struct {
	ID string `json:"id"`

	// APIKey stores the SHA-256 hash of the raw API key.
	// The plaintext key is never persisted — only returned once on creation/rotation.
	APIKey string `json:"api_key_hash"`

	// EC-08: dual-key rotation fields — old key hash stays valid until OldKeyExpiresAt.
	OldAPIKey       string     `json:"old_api_key_hash,omitempty"`
	OldKeyExpiresAt *time.Time `json:"old_key_expires_at,omitempty"`

	RPS    int    `json:"rps"`
	Burst  int    `json:"burst"`
	Status string `json:"status"`

	SuspendedAt      *time.Time `json:"suspended_at"`
	SuspensionReason string     `json:"suspension_reason"`

	IPAllowlist []string  `json:"ip_allowlist"`
	CreatedAt   time.Time `json:"created_at"`

	// Identity and lifecycle fields (control plane)
	Name        string         `json:"name,omitempty"`
	Slug        string         `json:"slug,omitempty"`
	Email       string         `json:"email,omitempty"`
	Company     string         `json:"company,omitempty"`
	Plan        string         `json:"plan,omitempty"`
	MaxLicenses int            `json:"max_licenses,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   *time.Time     `json:"deleted_at,omitempty"`
}

// IsSuspended returns true if the tenant is not allowed to make requests.
func (t *Tenant) IsSuspended() bool {
	return t.Status == "suspended" || t.Status == "deleted"
}

// AcceptsKey returns true if the given key hash matches this tenant's current
// or (during grace period) rotated-away API key hash.
// Callers MUST pass security.HashAPIKey(rawKey) — never the plaintext key.
func (t *Tenant) AcceptsKey(keyHash string) bool {
	if t.APIKey == keyHash {
		return true
	}
	if t.OldAPIKey == keyHash && t.OldKeyExpiresAt != nil && time.Now().Before(*t.OldKeyExpiresAt) {
		return true
	}
	return false
}

// AllowsAllIPs returns true if no IP restriction is configured.
func (t *Tenant) AllowsAllIPs() bool {
	// A nil slice means no restriction (database uses NULL).
	return t.IPAllowlist == nil
}

// ActivationRecord records a seat activation for a license.
type ActivationRecord struct {
	ID string `json:"id"`

	LicenseID string `json:"license_id"`
	TenantID  string `json:"tenant_id"`

	ClientID string `json:"client_id"`
	Hostname string `json:"hostname"`

	IsActive bool `json:"is_active"`

	ActivatedAt time.Time  `json:"activated_at"`
	ReleasedAt  *time.Time `json:"released_at"`

	// Diagnostics and device fingerprint evolution
	IP        string         `json:"ip,omitempty"`
	UserAgent string         `json:"user_agent,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// UsageRecord tracks consumption-based usage events.
type UsageRecord struct {
	ID string `json:"id"`

	LicenseID string `json:"license_id"`
	TenantID  string `json:"tenant_id"`

	Units int `json:"units"`

	RecordedAt time.Time `json:"recorded_at"`

	// Aggregation-friendly context
	Source   string         `json:"source,omitempty"` // api | batch | manual
	Metadata map[string]any `json:"metadata,omitempty"`
}
