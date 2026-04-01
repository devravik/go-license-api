package handlers

import (
	"github.com/devravik/go-license-api/internal/http/dto"
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

	result, err := h.ValidationService.Validate(c.Context(), req.Key, req.Product)
	if err != nil {
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
}
