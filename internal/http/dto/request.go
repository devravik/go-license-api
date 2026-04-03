package dto

type LicenseValidationRequest struct {
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
	ClientID   string `json:"client_id"`
	MachineID  string `json:"machine_id"`
	Product    string `json:"product"`
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
