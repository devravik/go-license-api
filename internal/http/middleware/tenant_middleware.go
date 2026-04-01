package middleware

import (
	"github.com/gofiber/fiber/v3"
)

func Tenant(c fiber.Ctx) error {
	// TODO: Extract Tenant ID from headers
	return c.Next()
}
