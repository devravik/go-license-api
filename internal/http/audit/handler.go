package audit

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

func (h *Handler) Query(c fiber.Ctx) error {
	return h.base.AdminQueryAuditLog(c)
}

