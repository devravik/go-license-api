package handlers

import (
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) Home(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"message": h.Cfg.AppName,
		"status":  "ok",
	})
}
