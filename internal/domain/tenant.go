package domain

import "time"

// Tenant represents a registered API consumer.
type Tenant struct {
	ID string `json:"id"`

	APIKey string `json:"api_key"`

	// EC-08: dual-key rotation fields — old key stays valid until OldKeyExpiresAt.
	OldAPIKey       string     `json:"old_api_key,omitempty"`
	OldKeyExpiresAt *time.Time `json:"old_key_expires_at,omitempty"`

	RPS    int    `json:"rps"`
	Burst  int    `json:"burst"`
	Status string `json:"status"`

	SuspendedAt      *time.Time `json:"suspended_at"`
	SuspensionReason string     `json:"suspension_reason"`

	IPAllowlist []string  `json:"ip_allowlist"`
	CreatedAt   time.Time `json:"created_at"`
}

// IsSuspended returns true if the tenant is not allowed to make requests.
func (t *Tenant) IsSuspended() bool {
	return t.Status == "suspended" || t.Status == "deleted"
}

// AcceptsKey returns true if the given API key is valid for this tenant.
// EC-08: checks both the current key and a rotated-away key during its grace period.
func (t *Tenant) AcceptsKey(key string) bool {
	if t.APIKey == key {
		return true
	}
	if t.OldAPIKey == key && t.OldKeyExpiresAt != nil && time.Now().Before(*t.OldKeyExpiresAt) {
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

	LicenseID int    `json:"license_id"`
	TenantID  string `json:"tenant_id"`

	MachineID string `json:"machine_id"`
	Hostname  string `json:"hostname"`

	IsActive bool `json:"is_active"`

	ActivatedAt time.Time  `json:"activated_at"`
	ReleasedAt  *time.Time `json:"released_at"`
}
