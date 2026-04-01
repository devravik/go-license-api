package dto

type LicenseValidationRequest struct {
	Key     string `json:"key" validate:"required"`
	Product string `json:"product"`
}
