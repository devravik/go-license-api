package handlers

import (
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) GetSignedLicense(c fiber.Ctx) error {
	key := c.Params("key")
	license, err := h.LicenseStore.GetByGlobalKey(c.Context(), key)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(dto.ErrorEnvelope{
			Success: false,
			Error:   dto.NewError("license_not_found", "License not found"),
		})
	}

	signer := h.SignerRegistry.For(c.Context(), license.TenantID)
	signed, err := signer.Sign(c.Context(), license)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(dto.ErrorEnvelope{
			Success: false,
			Error:   dto.NewError("signing_failed", "Signing failed"),
		})
	}

	c.Set("Content-Type", "application/json")
	c.Set("Cache-Control", "public, max-age=300")
	return c.Send(signed)
}

