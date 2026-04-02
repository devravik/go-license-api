package handlers

import (
	"github.com/devravik/go-license-api/internal/infrastructure/crypto"
	"github.com/google/uuid"
	"time"

	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) AdminStatus(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"message": "Admin API - Operational",
	})
}

func (h *Handler) AdminCreateTenant(c fiber.Ctx) error {
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

	tenant, apiKey, err := h.AdminService.CreateTenant(c.Context(), req.RPS, req.Burst)
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

func (h *Handler) AdminRevokeLicense(c fiber.Ctx) error {
	var req dto.AdminRevokeLicenseRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if req.TenantID == "" || req.Key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_and_key_required"})
	}
	if err := h.AdminService.RevokeLicense(c.Context(), req.TenantID, req.Key); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) AdminSuspendTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminSuspendTenantRequest
	_ = c.Bind().Body(&req)
	if err := h.AdminService.SuspendTenant(c.Context(), tenantID, req.Reason); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) AdminReinstateTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	if err := h.AdminService.ReinstateTenant(c.Context(), tenantID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "active"})
}

func (h *Handler) AdminDeleteTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	if err := h.AdminService.DeleteTenant(c.Context(), tenantID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *Handler) AdminRotateTenantKey(c fiber.Ctx) error {
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

	newKey, expiresAt, err := h.AdminService.RotateTenantAPIKey(c.Context(), tenantID, time.Duration(req.GraceMinutes)*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"new_api_key":              newKey,
		"old_key_grace_expires_at": expiresAt,
	})
}

func (h *Handler) AdminUpdateTenantLimits(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminUpdateTenantLimitsRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if err := h.AdminService.UpdateTenantLimits(c.Context(), tenantID, req.RPS, req.Burst); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

func (h *Handler) AdminUpdateTenantIPAllowlist(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminUpdateTenantIPAllowlistRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if err := h.AdminService.UpdateTenantIPAllowlist(c.Context(), tenantID, req.CIDRs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

// AdminRegisterWebhook registers a webhook for a tenant. Control plane only.
func (h *Handler) AdminRegisterWebhook(c fiber.Ctx) error {
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
	if len(h.WebhookEncKey) != 32 {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "webhook_encryption_key_invalid"})
	}
	enc, err := crypto.EncryptAES(h.WebhookEncKey, []byte(req.Secret))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "encryption_failed"})
	}
	if h.WebhookRepo == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "webhook_repo_unavailable"})
	}
	if err := h.WebhookRepo.Create(c.Context(), uuid.New().String(), tenantID, req.URL, req.Events, enc); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed_to_register"})
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
}
