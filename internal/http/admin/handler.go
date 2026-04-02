package admin

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

func (h *Handler) Status(c fiber.Ctx) error { return h.base.AdminStatus(c) }
func (h *Handler) CreateTenant(c fiber.Ctx) error { return h.base.AdminCreateTenant(c) }
func (h *Handler) RevokeLicense(c fiber.Ctx) error { return h.base.AdminRevokeLicense(c) }
func (h *Handler) SuspendTenant(c fiber.Ctx) error { return h.base.AdminSuspendTenant(c) }
func (h *Handler) ReinstateTenant(c fiber.Ctx) error { return h.base.AdminReinstateTenant(c) }
func (h *Handler) UpdateTenantIPAllowlist(c fiber.Ctx) error { return h.base.AdminUpdateTenantIPAllowlist(c) }
func (h *Handler) RegisterWebhook(c fiber.Ctx) error { return h.base.AdminRegisterWebhook(c) }
func (h *Handler) RotateTenantKey(c fiber.Ctx) error { return h.base.AdminRotateTenantKey(c) }
func (h *Handler) UpdateTenantLimits(c fiber.Ctx) error { return h.base.AdminUpdateTenantLimits(c) }
func (h *Handler) DeleteTenant(c fiber.Ctx) error { return h.base.AdminDeleteTenant(c) }

