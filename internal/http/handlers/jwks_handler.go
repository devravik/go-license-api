package handlers

import "github.com/gofiber/fiber/v3"

func (h *Handler) JWKS(c fiber.Ctx) error {
	c.Set("Cache-Control", "public, max-age=3600")
	return c.JSON(h.SignerRegistry.JWKS())
}

