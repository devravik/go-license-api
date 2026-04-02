package handlers

import (
	"context"

	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) Validate(c fiber.Ctx) error {
	var req dto.LicenseValidationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.LicenseValidationResponse{
			Valid: false,
			Error: "invalid_request_body",
		})
	}

	if req.Key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.LicenseValidationResponse{
			Valid: false,
			Error: "key_is_required",
		})
	}

	apiKey, _ := c.Locals("api_key").(string)
	resultCh := make(chan worker.Result, 1)
	job := &worker.ValidateJob{
		APIKey:     apiKey,
		LicenseKey: req.Key,
		Product:    req.Product,
		Ctx:        context.Background(),
		ResultCh:   resultCh,
	}

	if !h.Pool.Enqueue(job) {
		c.Set("Retry-After", "5")
		return c.Status(fiber.StatusServiceUnavailable).JSON(dto.LicenseValidationResponse{
			Valid: false,
			Error: "service_unavailable",
		})
	}

	select {
	case result := <-resultCh:
		if result.Err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(dto.LicenseValidationResponse{
				Valid: false,
				Error: "internal_validation_error",
			})
		}
		return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
			Valid: result.Valid,
			Meta:  result.Meta,
			Error: result.Error,
		})
	case <-c.Context().Done():
		return c.Status(fiber.StatusRequestTimeout).JSON(dto.LicenseValidationResponse{
			Valid: false,
			Error: "request_timeout",
		})
	}
}
