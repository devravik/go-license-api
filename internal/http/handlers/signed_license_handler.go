package handlers

import (
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) GetSignedLicense(c fiber.Ctx) error {
	key := c.Params("key")
	tenant := middleware.TenantFromCtx(c)

	license, err := h.LicenseStore.Get(c.Context(), tenant.ID, key)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "license_not_found"})
	}

	signer := h.SignerRegistry.For(c.Context(), tenant.ID)
	signed, err := signer.Sign(c.Context(), license)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "signing_failed"})
	}

	c.Set("Content-Type", "application/json")
	c.Set("Cache-Control", "public, max-age=300")
	return c.Send(signed)
}

