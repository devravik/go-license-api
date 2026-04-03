package handlers

import (
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) GetSignedLicense(c fiber.Ctx) error {
	key := c.Params("key")
	tenant := middleware.TenantFromCtx(c)

	license, err := h.LicenseStore.Get(c.Context(), tenant.ID, key)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(dto.ErrorEnvelope{
			Success: false,
			Error:   dto.NewError("license_not_found", "License not found"),
		})
	}

	signer := h.SignerRegistry.For(c.Context(), tenant.ID)
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

// PostSignedLicense issues a signed license by JSON body.
func (h *Handler) PostSignedLicense(c fiber.Ctx) error {
	tenant := middleware.TenantFromCtx(c)
	var req dto.SignedLicenseRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.ErrorEnvelope{
			Success: false,
			Error:   dto.NewError("invalid_request_body", "Invalid request body"),
		})
	}
	key := req.EffectiveLicenseKey()
	if key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.ErrorEnvelope{
			Success: false,
			Error:   dto.NewError("key_is_required", "license_key is required"),
		})
	}
	license, err := h.LicenseStore.Get(c.Context(), tenant.ID, key)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(dto.ErrorEnvelope{
			Success: false,
			Error:   dto.NewError("license_not_found", "License not found"),
		})
	}
	signer := h.SignerRegistry.For(c.Context(), tenant.ID)
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

