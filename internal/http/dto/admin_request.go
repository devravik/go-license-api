package dto

type AdminRevokeLicenseRequest struct {
	TenantID string `json:"tenant_id"`
	Key      string `json:"key"`
	Reason   string `json:"reason,omitempty"`
}

type AdminCreateTenantRequest struct {
	RPS   int `json:"rps"`
	Burst int `json:"burst"`
}

type AdminSuspendTenantRequest struct {
	Reason string `json:"reason"`
}

type AdminUpdateTenantLimitsRequest struct {
	RPS   int `json:"rps"`
	Burst int `json:"burst"`
}

type AdminUpdateTenantIPAllowlistRequest struct {
	CIDRs []string `json:"cidrs"`
}

type AdminRotateAPIKeyRequest struct {
	GraceMinutes int `json:"grace_minutes"`
}

type AdminRegisterWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Secret string   `json:"secret"`
}

type AdminUpdateTenantProfileRequest struct {
	Name        string                 `json:"name"`
	Slug        string                 `json:"slug"`
	Email       string                 `json:"email"`
	Company     string                 `json:"company"`
	Plan        string                 `json:"plan"`
	MaxLicenses int                    `json:"max_licenses"`
	Metadata    map[string]any         `json:"metadata"`
}
