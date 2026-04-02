package handlers

import (
	"time"

	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) AdminStatus(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"message": "Admin API - Operational",
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

func (h *Handler) AdminRotateTenantKey(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
	}
	var req dto.AdminRotateAPIKeyRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	if req.NewKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "new_key_required"})
	}
	grace := req.GracePeriod
	if grace == 0 {
		grace = 10 * time.Minute
	}
	if err := h.AdminService.RotateTenantAPIKey(c.Context(), tenantID, req.NewKey, grace); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}
