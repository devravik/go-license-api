package handlers

import (
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) Health(c fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(dto.HealthResponse{
		Status: "up",
	})
}
