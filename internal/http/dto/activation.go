package dto

type ActivateRequest struct {
	Key       string `json:"key"`
	MachineID string `json:"machine_id"`
	Hostname  string `json:"hostname"`
}

type ActivateResponse struct {
	Activated      bool   `json:"activated"`
	ActivationID   string `json:"activation_id,omitempty"`
	SeatsRemaining *int   `json:"seats_remaining,omitempty"`
	Error          string `json:"error,omitempty"`
}

type DeactivateRequest struct {
	Key          string `json:"key"`
	ActivationID string `json:"activation_id"`
}

type DeactivateResponse struct {
	Deactivated bool   `json:"deactivated"`
	Error       string `json:"error,omitempty"`
}

// Usage tracking
type UsageRequest struct {
	Key   string `json:"key"`
	Units int    `json:"units"`
}

type UsageResponse struct {
	Recorded bool   `json:"recorded"`
	Error    string `json:"error,omitempty"`
}
