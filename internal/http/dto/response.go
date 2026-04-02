package dto

import "github.com/devravik/go-license-api/internal/domain"

type LicenseValidationResponse struct {
	Valid bool                   `json:"valid"`
	Meta  *domain.ValidationMeta `json:"meta,omitempty"`
	Error string                 `json:"error,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
}
