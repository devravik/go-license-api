package domain

import "time"

// Product is a tenant-scoped, cache-friendly entity that describes a product's
// feature set and validation context. Control-plane only; data-plane must read
// from cache (never DB) for product checks.
type Product struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`

	// Code is the stable identifier used in API requests (e.g. "wordpress-plugin").
	Code string `json:"code"`
	Name string `json:"name"`

	// Version is optional (e.g. "v1", "v2") for backward-compatible licensing.
	Version *string `json:"version,omitempty"`

	IsActive bool `json:"is_active"`

	// Features is a JSON array of feature flags, intended to be small.
	Features []string       `json:"features"`
	Meta     map[string]any `json:"meta"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (p *Product) HasFeature(name string) bool {
	for _, f := range p.Features {
		if f == name {
			return true
		}
	}
	return false
}
