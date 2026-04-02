package admin

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/devravik/go-license-api/internal/http/dto"
	crypto "github.com/devravik/go-license-api/internal/security"
	"github.com/google/uuid"
	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	base *handlers.Handler
}

func NewHandler(base *handlers.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Status(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"message": "Admin API - Operational",
	})
}

func (h *Handler) CreateTenant(c fiber.Ctx) error {
	var req dto.AdminCreateTenantRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if req.RPS == 0 {
		req.RPS = 100
	}
	if req.Burst == 0 {
		req.Burst = 200
	}

	tenant, apiKey, err := h.base.AdminService.CreateTenant(c.Context(), req.RPS, req.Burst)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"tenant_id": tenant.ID,
		"api_key":   apiKey,
		"limits": fiber.Map{
			"rps":   tenant.RPS,
			"burst": tenant.Burst,
		},
	})
}

func (h *Handler) RevokeLicense(c fiber.Ctx) error {
	var req dto.AdminRevokeLicenseRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if req.TenantID == "" || req.Key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_and_key_required"})
	}
	// Backward-compatible: AdminService interface keeps original signature (no reason).
	if err := h.base.AdminService.RevokeLicense(c.Context(), req.TenantID, req.Key); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) SuspendTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminSuspendTenantRequest
	_ = c.Bind().Body(&req)
	if err := h.base.AdminService.SuspendTenant(c.Context(), tenantID, req.Reason); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) ReinstateTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	if err := h.base.AdminService.ReinstateTenant(c.Context(), tenantID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "active"})
}

func (h *Handler) DeleteTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	if err := h.base.AdminService.DeleteTenant(c.Context(), tenantID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *Handler) RotateTenantKey(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminRotateAPIKeyRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if req.GraceMinutes == 0 {
		req.GraceMinutes = 60
	}
	if req.GraceMinutes > 1440 {
		req.GraceMinutes = 1440
	}

	newKey, expiresAt, err := h.base.AdminService.RotateTenantAPIKey(c.Context(), tenantID, time.Duration(req.GraceMinutes)*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"new_api_key":              newKey,
		"old_key_grace_expires_at": expiresAt,
	})
}

func (h *Handler) UpdateTenantLimits(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminUpdateTenantLimitsRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if err := h.base.AdminService.UpdateTenantLimits(c.Context(), tenantID, req.RPS, req.Burst); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

func (h *Handler) UpdateTenantIPAllowlist(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminUpdateTenantIPAllowlistRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if err := h.base.AdminService.UpdateTenantIPAllowlist(c.Context(), tenantID, req.CIDRs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

// AdminRegisterWebhook registers a webhook for a tenant. Control plane only.
func (h *Handler) RegisterWebhook(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminRegisterWebhookRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if req.URL == "" || len(req.Events) == 0 || req.Secret == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "url_events_secret_required"})
	}
	if len(h.base.WebhookEncKey) != 32 {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "webhook_encryption_key_invalid"})
	}
	enc, err := crypto.EncryptAES(h.base.WebhookEncKey, []byte(req.Secret))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "encryption_failed"})
	}
	if h.base.WebhookRepo == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "webhook_repo_unavailable"})
	}
	if err := h.base.WebhookRepo.Create(c.Context(), uuid.New().String(), tenantID, req.URL, req.Events, enc); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed_to_register"})
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
}

// UpdateTenantProfile updates tenant identity and billing-profile fields if supported by the service.
func (h *Handler) UpdateTenantProfile(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminUpdateTenantProfileRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	// Optional method detection via type assertion.
	type profiler interface {
		UpdateTenantProfile(ctx fiber.Ctx, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	}
	// Use the underlying concrete value; AdminService may wrap the concrete type.
	if svc, ok := any(h.base.AdminService).(interface {
		UpdateTenantProfile(ctx context.Context, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	}); ok {
		if err := svc.UpdateTenantProfile(c.Context(), tenantID, req.Name, req.Slug, req.Email, req.Company, req.Plan, req.MaxLicenses, req.Metadata); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "updated"})
	}
	return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "update_profile_not_supported"})
}


