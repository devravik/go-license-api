package domain

import "time"

// License represents a single license record as stored in the database.
type License struct {
	ID              int            `json:"id"`
	TenantID        string         `json:"tenant_id"`
	Key             string         `json:"key"`
	ProductID       string         `json:"product_id"`
	Product         string         `json:"product"`
	Status          string         `json:"status"` // active | revoked | expired
	Plan            string         `json:"plan"`
	IsTrial         bool           `json:"is_trial"`
	TrialEndsAt     *time.Time     `json:"trial_ends_at"`
	ExpiresAt       *time.Time     `json:"expires_at"`
	GracePeriodDays int            `json:"grace_period_days"`
	SeatCount       *int           `json:"seat_count"` // nil = unlimited
	MaxActivations  *int           `json:"max_activations"`
	UsageLimit      *int           `json:"usage_limit"`
	UsageUsed       int            `json:"usage_used"`
	Features        []string       `json:"features"`
	Meta            map[string]any `json:"meta"`
	CreatedAt       time.Time      `json:"created_at"`
}

// ValidationMeta is the structured metadata for successful validations.
type ValidationMeta struct {
	Plan              string     `json:"plan,omitempty"`
	Product           string     `json:"product,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	SeatsTotal        *int       `json:"seats_total,omitempty"`
	Trial             bool       `json:"trial,omitempty"`
	GracePeriodEndsAt *time.Time `json:"grace_period_ends_at,omitempty"`
	Features          []string   `json:"features,omitempty"`
	InGracePeriod     bool       `json:"in_grace_period,omitempty"`
}

// IsExpired returns true if the license has passed its expiry and grace period.
func (l *License) IsExpired() bool {
	if l.ExpiresAt == nil {
		return false
	}
	gracePeriodEnd := l.ExpiresAt.AddDate(0, 0, l.GracePeriodDays)
	return time.Now().After(gracePeriodEnd)
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
