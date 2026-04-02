package dto

import "time"

type AdminRevokeLicenseRequest struct {
	TenantID string `json:"tenant_id"`
	Key      string `json:"key"`
}

type AdminSuspendTenantRequest struct {
	Reason string `json:"reason"`
}

type AdminRotateAPIKeyRequest struct {
	NewKey      string        `json:"new_key"`
	GracePeriod time.Duration `json:"grace_period"`
}
