package handlers

import (
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
	"strings"
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

	// If binding is required by policy and no client_id provided, reject early.
	if license.Metadata != nil {
		if v, ok := license.Metadata["binding_required"]; ok {
			if b, ok2 := v.(bool); ok2 && b {
				if strings.TrimSpace(c.Query("client_id")) == "" {
					return c.Status(fiber.StatusBadRequest).JSON(dto.ErrorEnvelope{
						Success: false,
						Error:   dto.NewError("client_id_required", "client_id is required when binding_required=true"),
					})
				}
			}
		}
	}

	// Optional soft-binding: if client_id is provided and an active activation exists,
	// include activation_id and client_id inside the signed payload (via transient metadata).
	if clientID := strings.TrimSpace(c.Query("client_id")); clientID != "" && h.ActivationService != nil {
		if act, aerr := h.ActivationService.GetActiveByClient(c.Context(), license.TenantID, license.Key, clientID); aerr == nil && act != nil {
			if license.Metadata == nil {
				license.Metadata = map[string]any{}
			}
			license.Metadata["_activation_id"] = act.ID
			license.Metadata["_client_id"] = act.ClientID
		}
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
