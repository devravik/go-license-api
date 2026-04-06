package tenant

import (
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	h *handlers.Handler
}

func NewHandler(h *handlers.Handler) *Handler {
	return &Handler{h: h}
}

// License endpoints (tenant-scoped). Stubbed for now; to be implemented by services.
func (t *Handler) CreateLicense(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}
func (t *Handler) GetLicense(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}
func (t *Handler) UpdateLicense(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}
func (t *Handler) RevokeLicense(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}

// Product endpoints (tenant-scoped). Stubbed for now.
func (t *Handler) UpsertProduct(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}
func (t *Handler) GetProduct(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}
func (t *Handler) UpdateProduct(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}
func (t *Handler) DeleteProduct(c fiber.Ctx) error {
	return errJSON(c, fiber.StatusNotImplemented, "not_implemented")
}

func errJSON(c fiber.Ctx, status int, code string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": dto.NewError(code, tenantErrorMessage(code)),
	})
}

func tenantErrorMessage(code string) string {
	if code == "not_implemented" {
		return "Not implemented"
	}
	return code
}
