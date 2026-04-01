package handlers

import (
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) AdminDashboard(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"message": "Admin Dashboard - Under Construction",
	})
}
