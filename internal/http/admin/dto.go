package admin

import "github.com/devravik/go-license-api/internal/http/dto"

type RevokeLicenseRequest = dto.AdminRevokeLicenseRequest
type CreateTenantRequest = dto.AdminCreateTenantRequest
type SuspendTenantRequest = dto.AdminSuspendTenantRequest
type UpdateTenantLimitsRequest = dto.AdminUpdateTenantLimitsRequest
type UpdateTenantIPAllowlistRequest = dto.AdminUpdateTenantIPAllowlistRequest
type RotateAPIKeyRequest = dto.AdminRotateAPIKeyRequest
type RegisterWebhookRequest = dto.AdminRegisterWebhookRequest

