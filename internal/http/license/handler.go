package license

import (
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	base *handlers.Handler
}

func NewHandler(base *handlers.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Validate(c fiber.Ctx) error {
	return h.base.Validate(c)
}

func (h *Handler) Activate(c fiber.Ctx) error {
	return h.base.Activate(c)
}

func (h *Handler) Deactivate(c fiber.Ctx) error {
	return h.base.Deactivate(c)
}

