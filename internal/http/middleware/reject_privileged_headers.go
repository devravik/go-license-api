package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// RejectPrivilegedHeaders rejects any request that attempts to pass
// control-plane credentials to public data-plane endpoints.
// It is intentionally strict and returns 400 to signal a bad request.
func RejectPrivilegedHeaders() fiber.Handler {
	return func(c fiber.Ctx) error {
		if hasHeader(c, "X-Admin-Key") {
			return c.Status(fiber.StatusBadRequest).JSON(errorResponse("privileged_headers_not_allowed", "Privileged headers not allowed on public endpoints"))
		}
		return c.Next()
	}
}

func hasHeader(c fiber.Ctx, name string) bool {
	v := strings.TrimSpace(c.Get(name))
	return v != ""
}
