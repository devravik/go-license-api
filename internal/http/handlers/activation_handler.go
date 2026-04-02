package handlers

import (
	"errors"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) Activate(c fiber.Ctx) error {
	if h.ActivationService == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(dto.ActivateResponse{
			Activated: false,
			Error:     "activation_not_enabled",
		})
	}

	tenantID, _ := c.Locals("tenant_id").(string)
	idempotencyKey := c.Get("Idempotency-Key")
	if idempotencyKey != "" && h.IdempCache != nil {
		if cached, ok := h.IdempCache.Get(tenantID, idempotencyKey); ok {
			return c.JSON(cached)
		}
	}

	var req dto.ActivateRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.ActivateResponse{
			Activated: false,
			Error:     "invalid_request",
		})
	}
	if req.Key == "" || req.MachineID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.ActivateResponse{
			Activated: false,
			Error:     "key_and_machine_id_required",
		})
	}

	record, remaining, err := h.ActivationService.Activate(c.Context(), tenantID, req.Key, req.MachineID, req.Hostname)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrSeatLimitReached):
			return c.Status(fiber.StatusForbidden).JSON(dto.ActivateResponse{Activated: false, Error: "seat_limit_reached"})
		case errors.Is(err, domain.ErrLicenseNotFound):
			return c.Status(fiber.StatusNotFound).JSON(dto.ActivateResponse{Activated: false, Error: "license_not_found"})
		case errors.Is(err, domain.ErrLicenseExpired):
			return c.Status(fiber.StatusPaymentRequired).JSON(dto.ActivateResponse{Activated: false, Error: "license_expired"})
		case errors.Is(err, domain.ErrLicenseRevoked):
			return c.Status(fiber.StatusForbidden).JSON(dto.ActivateResponse{Activated: false, Error: "license_revoked"})
		case errors.Is(err, domain.ErrLicenseGracePeriod):
			return c.Status(fiber.StatusForbidden).JSON(dto.ActivateResponse{Activated: false, Error: "license_in_grace_period"})
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(dto.ActivateResponse{Activated: false, Error: "internal_error"})
		}
	}

	resp := dto.ActivateResponse{
		Activated:    true,
		ActivationID: record.ID,
	}
	if remaining >= 0 {
		resp.SeatsRemaining = &remaining
	}

	if idempotencyKey != "" && h.IdempCache != nil {
		h.IdempCache.Set(tenantID, idempotencyKey, resp, 24*time.Hour)
	}
	return c.JSON(resp)
}

func (h *Handler) Deactivate(c fiber.Ctx) error {
	if h.ActivationService == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(dto.DeactivateResponse{
			Deactivated: false,
			Error:       "activation_not_enabled",
		})
	}

	tenantID, _ := c.Locals("tenant_id").(string)

	var req dto.DeactivateRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.DeactivateResponse{
			Deactivated: false,
			Error:       "invalid_request",
		})
	}
	if req.Key == "" || req.ActivationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.DeactivateResponse{
			Deactivated: false,
			Error:       "key_and_activation_id_required",
		})
	}

	if err := h.ActivationService.Deactivate(c.Context(), tenantID, req.Key, req.ActivationID); err != nil {
		if errors.Is(err, domain.ErrLicenseNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(dto.DeactivateResponse{
				Deactivated: false,
				Error:       "license_not_found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(dto.DeactivateResponse{
			Deactivated: false,
			Error:       "internal_error",
		})
	}
	return c.JSON(dto.DeactivateResponse{Deactivated: true})
}
