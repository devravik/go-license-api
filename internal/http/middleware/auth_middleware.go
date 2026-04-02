package middleware

import (
	"strings"

	"github.com/devravik/go-license-api/configs"
	"github.com/gofiber/fiber/v3"
)

func Auth(c fiber.Ctx) error {
	cfg := configs.Load()
	adminKey := c.Get("X-Admin-Key")
	if strings.TrimSpace(adminKey) == "" || adminKey != cfg.AdminKey {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "invalid_admin_key",
		})
	}
	return c.Next()
}
