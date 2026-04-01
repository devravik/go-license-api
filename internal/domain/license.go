package domain

import "time"

type License struct {
	ID        int            `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Key       string         `json:"key"`
	Product   string         `json:"product"`
	Status    string         `json:"status"`
	ExpiresAt time.Time      `json:"expires_at"`
	Meta      map[string]any `json:"meta"`
}

type ValidationResult struct {
	Valid bool           `json:"valid"`
	Meta  map[string]any `json:"meta,omitempty"`
	Error string         `json:"error,omitempty"`
}
