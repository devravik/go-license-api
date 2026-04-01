package middleware

import (
	"github.com/gofiber/fiber/v3"
)

func RateLimit(c fiber.Ctx) error {
	// TODO: Enforce tenant specific rate limits
	return c.Next()
}
