package dto

type LicenseValidationRequest struct {
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
	ProductID  string `json:"product_id"`
	// Deprecated alias; treated as product_id.
	Product string `json:"product"`
}

func (r LicenseValidationRequest) EffectiveLicenseKey() string {
	if r.LicenseKey != "" {
		return r.LicenseKey
	}
	return r.Key
}

func (r LicenseValidationRequest) EffectiveProductID() string {
	if r.ProductID != "" {
		return r.ProductID
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
