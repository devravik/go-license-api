package middleware

import (
	"github.com/gofiber/fiber/v3"
)

func Auth(c fiber.Ctx) error {
	// TODO: Validate API Key or Admin Key
	return c.Next()
}
