package dto

type LicenseValidationResponse struct {
	Valid bool           `json:"valid"`
	Meta  map[string]any `json:"meta,omitempty"`
	Error string         `json:"error,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
}
