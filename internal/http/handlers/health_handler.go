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

func (h *Handler) Ready(c fiber.Ctx) error {
	ready := true
	if h != nil && h.IsReady != nil {
		ready = h.IsReady()
	}
	if !ready {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "starting",
		})
	}
	depth := 0
	if h != nil && h.QueueDepth != nil {
		depth = h.QueueDepth()
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":      "ready",
		"queue_depth": depth,
	})
}
