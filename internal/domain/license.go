package domain

import "time"

// License represents a single license record as stored in the database.
type License struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	Type            string          `json:"type"` // plan | product
	PlanID          *string         `json:"plan_id,omitempty"`
	ProductID       *string         `json:"product_id,omitempty"`
	Key             string          `json:"key"`
	Status          string          `json:"status"` // active | expired | revoked
	NotBefore       *time.Time      `json:"not_before,omitempty"`
	ExpiresAt       *time.Time      `json:"expires_at,omitempty"`
	SeatsTotal      int             `json:"seats_total"` // -1 => unlimited
	SeatsUsed       int             `json:"seats_used"`
	Features        []string        `json:"features"`
	Overrides       LicenseOverride `json:"overrides"`
	Trial           LicenseTrial    `json:"trial"`
	Metadata        map[string]any  `json:"metadata"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	FinalFeatures   []string        `json:"final_features,omitempty"`
	GracePeriodDays int             `json:"grace_period_days,omitempty"`
	MaxActivations  *int            `json:"max_activations,omitempty"`
	UsageLimit      *int            `json:"usage_limit,omitempty"`
	UsageUsed       int             `json:"usage_used,omitempty"`
	// Legacy compatibility fields kept for older handlers/tests.
	Product         string         `json:"product,omitempty"`
	Plan            string         `json:"plan,omitempty"`
	IsTrial         bool           `json:"is_trial,omitempty"`
	TrialEndsAt     *time.Time     `json:"trial_ends_at,omitempty"`
	SeatCount       *int           `json:"seat_count,omitempty"`
	Meta            map[string]any `json:"meta,omitempty"`
	IssuedAt        time.Time      `json:"issued_at,omitempty"`
	RevokedAt       *time.Time     `json:"revoked_at,omitempty"`
	RevokedReason   string         `json:"revoked_reason,omitempty"`
	RevocationID    string         `json:"revocation_id,omitempty"`
	LastValidatedAt *time.Time     `json:"last_validated_at,omitempty"`
	Version         int            `json:"version,omitempty"`
	DeletedAt       *time.Time     `json:"deleted_at,omitempty"`
}

type LicenseOverride struct {
	FeaturesAdd    []string `json:"features_add,omitempty"`
	FeaturesRemove []string `json:"features_remove,omitempty"`
}

type LicenseTrial struct {
	Enabled  bool       `json:"enabled"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
	Features []string   `json:"features,omitempty"`
}

// ValidationMeta is the structured metadata for successful validations.
type ValidationMeta struct {
	LicenseID         string         `json:"license_id,omitempty"`
	Status            string         `json:"status,omitempty"`
	Type              string         `json:"type"`
	PlanID            string         `json:"plan_id"`
	Plan              *ValidationRef `json:"plan,omitempty"`
	Product           *ValidationRef `json:"product,omitempty"`
	ProductID         string         `json:"product_id"`
	NotBefore         *time.Time     `json:"not_before,omitempty"`
	ExpiresAt         *time.Time     `json:"expires_at,omitempty"`
	SeatsTotal        *int           `json:"seats_total,omitempty"`
	UnlimitedSeats    bool           `json:"unlimited_seats"`
	Trial             bool           `json:"trial,omitempty"`
	GracePeriodEndsAt *time.Time     `json:"grace_period_ends_at,omitempty"`
	Features          []string       `json:"features,omitempty"`
	Version           int            `json:"version,omitempty"`
	InGracePeriod     bool           `json:"in_grace_period,omitempty"`
}

type ValidationRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// IsExpired returns true if the license has passed its expiry and grace period.
func (l *License) IsExpired() bool {
	if l.ExpiresAt == nil {
		return false
	}
	end := *l.ExpiresAt
	if l.GracePeriodDays > 0 {
		end = end.AddDate(0, 0, l.GracePeriodDays)
	}
	return time.Now().After(end)
}

// IsInGracePeriod returns true if expired but still within the grace window.
func (l *License) IsInGracePeriod() bool {
	if l.ExpiresAt == nil {
		return false
	}
	now := time.Now()
	if !now.After(*l.ExpiresAt) {
		return false // not even expired yet
	}
	gracePeriodEnd := l.ExpiresAt.AddDate(0, 0, l.GracePeriodDays)
	return now.Before(gracePeriodEnd)
}

// GracePeriodEndsAt returns the computed grace period end timestamp.
func (l *License) GracePeriodEndsAt() *time.Time {
	if l.ExpiresAt == nil || l.GracePeriodDays == 0 {
		return nil
	}
	t := l.ExpiresAt.AddDate(0, 0, l.GracePeriodDays)
	return &t
}

// HasFeature checks if this license plan includes the named feature.
func (l *License) HasFeature(name string) bool {
	for _, f := range l.Features {
		if f == name {
			return true
		}
	}
	return false
}

// IsRevoked returns true if the license was administratively revoked.
func (l *License) IsRevoked() bool {
	return l.Status == "revoked"
}

// ValidationResult is the structured response returned by the validation use case.
type ValidationResult struct {
	Valid bool            `json:"valid"`
	Meta  *ValidationMeta `json:"meta,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Revocation is a compact representation for distribution to clients.
type Revocation struct {
	RevocationID string    `json:"revocation_id"`
	LicenseID    string    `json:"license_id"`
	RevokedAt    time.Time `json:"revoked_at"`
	Reason       string    `json:"reason,omitempty"`
}
