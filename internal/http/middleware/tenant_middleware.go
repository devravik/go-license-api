package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

func Tenant(c fiber.Ctx) error {
	apiKey := c.Get("X-Api-Key")
	if strings.TrimSpace(apiKey) == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"valid": false,
			"error": "missing_api_key",
		})
	}
	c.Locals("api_key", apiKey)
	return c.Next()
}
