package dto

type ActivateRequest struct {
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
	ClientID   string `json:"client_id"`
	MachineID  string `json:"machine_id"`
	Hostname   string `json:"hostname"`
}

type ActivateResponse struct {
	Success   bool   `json:"success,omitempty"`
	Activated bool   `json:"activated"`
	ClientID  string `json:"client_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Seats     *struct {
		Used      int `json:"used"`
		Total     int `json:"total"`
		Remaining int `json:"remaining"`
	} `json:"seats,omitempty"`
	Error *APIError `json:"error,omitempty"`
}

type DeactivateRequest struct {
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
	ClientID   string `json:"client_id"`
}

type DeactivateResponse struct {
	Success     bool      `json:"success,omitempty"`
	Deactivated bool   `json:"deactivated"`
	RequestID   string `json:"request_id,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
	Error       *APIError `json:"error,omitempty"`
}

// Usage tracking
type UsageRequest struct {
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
	Units      int    `json:"units"`
	EventID    string `json:"event_id"`
}

type UsageResponse struct {
	Success  bool `json:"success,omitempty"`
	Recorded bool   `json:"recorded"`
	RequestID string `json:"request_id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Usage     *struct {
		TotalUsed int  `json:"total_used"`
		Remaining *int `json:"remaining,omitempty"`
	} `json:"usage,omitempty"`
	Error    *APIError `json:"error,omitempty"`
}

func (r ActivateRequest) EffectiveLicenseKey() string {
	if r.LicenseKey != "" {
		return r.LicenseKey
	}
	return r.Key
}

func (r ActivateRequest) EffectiveClientID() string {
	if r.ClientID != "" {
		return r.ClientID
	}
	return r.MachineID
}

func (r DeactivateRequest) EffectiveLicenseKey() string {
	if r.LicenseKey != "" {
		return r.LicenseKey
	}
	return r.Key
}

func (r DeactivateRequest) EffectiveClientID() string {
	if r.ClientID != "" {
		return r.ClientID
	}
	return ""
}

func (r UsageRequest) EffectiveLicenseKey() string {
	if r.LicenseKey != "" {
		return r.LicenseKey
	}
	return r.Key
}
