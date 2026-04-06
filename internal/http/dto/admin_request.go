package dto

type AdminRevokeLicenseRequest struct {
	TenantID string `json:"tenant_id"`
	LicenseKey string `json:"license_key"`
	Key        string `json:"key"`
	Reason   string `json:"reason,omitempty"`
}

func (r AdminRevokeLicenseRequest) EffectiveLicenseKey() string {
	if r.LicenseKey != "" {
		return r.LicenseKey
	}
	return r.Key
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

type AdminUpsertPlanRequest struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	ProductID   *string  `json:"product_id"`
	Name        string   `json:"name"`
	Features    []string `json:"features"`
	Seats       int      `json:"seats"`
	IsActive    bool     `json:"is_active"`
}

type AdminSetPlanActiveRequest struct {
	Active bool `json:"active"`
}

type AdminCreateLicenseRequest struct {
	TenantID   string     `json:"tenant_id"`
	Type       string     `json:"type"`
	PlanID     *string    `json:"plan_id"`
	ProductID  *string    `json:"product_id"`
	Plan       *AdminInlinePlanInput    `json:"plan"`
	Product    *AdminInlineProductInput `json:"product"`
	Key        string     `json:"key"`
	Status     string     `json:"status"`
	ExpiresAt  *string    `json:"expires_at"`
	SeatsTotal int        `json:"seats_total"`
	SeatsUsed  int        `json:"seats_used"`
	Features   []string   `json:"features"`
	Overrides  struct {
		FeaturesAdd    []string `json:"features_add"`
		FeaturesRemove []string `json:"features_remove"`
	} `json:"overrides"`
	Trial struct {
		Enabled  bool     `json:"enabled"`
		EndsAt   *string  `json:"ends_at"`
		Features []string `json:"features"`
	} `json:"trial"`
	Metadata map[string]any `json:"metadata"`
}

type AdminUpdateLicenseRequest = AdminCreateLicenseRequest

type AdminInlineProductInput struct {
	Name     string   `json:"name"`
	Features []string `json:"features"`
}

type AdminInlinePlanInput struct {
	Name     string   `json:"name"`
	Features []string `json:"features"`
	Limits   struct {
		Seats int `json:"seats"`
	} `json:"limits"`
	Product *AdminInlineProductInput `json:"product"`
}
