package dto

import "github.com/devravik/go-license-api/internal/domain"

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Success bool      `json:"success"`
	Error   *APIError `json:"error"`
}

func NewError(code, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

type LicenseValidationResponse struct {
	Success   bool                   `json:"success,omitempty"`
	Valid     bool                   `json:"valid"`
	License   *domain.ValidationMeta `json:"license,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Timestamp string                 `json:"timestamp,omitempty"`
	Error     *APIError              `json:"error,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
}
