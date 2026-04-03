package dto

type LicenseValidationRequest struct {
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
	ClientID   string `json:"client_id"`
	MachineID  string `json:"machine_id"`
	// Preferred: product_code. Deprecated alias: product.
	ProductCode string `json:"product_code"`
	Product     string `json:"product"`
}

func (r LicenseValidationRequest) EffectiveLicenseKey() string {
	if r.LicenseKey != "" {
		return r.LicenseKey
	}
	return r.Key
}

func (r LicenseValidationRequest) EffectiveClientID() string {
	if r.ClientID != "" {
		return r.ClientID
	}
	return r.MachineID
}

// EffectiveProductCode returns the preferred product_code, falling back to legacy product.
func (r LicenseValidationRequest) EffectiveProductCode() string {
	if r.ProductCode != "" {
		return r.ProductCode
	}
	return r.Product
}
type SignedLicenseRequest struct {
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
}

func (r SignedLicenseRequest) EffectiveLicenseKey() string {
	if r.LicenseKey != "" {
		return r.LicenseKey
	}
	return r.Key
}
