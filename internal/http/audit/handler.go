package audit

import (
	"github.com/devravik/go-license-api/internal/audit"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/gofiber/fiber/v3"
	"strconv"
	"time"
)

type Handler struct {
	base *handlers.Handler
}

func NewHandler(base *handlers.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Query(c fiber.Ctx) error {
	if h.base.AuditQuery == nil {
		return errJSON(c, fiber.StatusServiceUnavailable, "audit_query_unavailable")
	}
	params := audit.QueryParams{
		TenantID: c.Query("tenant_id"),
		Event:    c.Query("event"),
	}
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			params.From = t
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			params.To = t
		}
	}
	if s := c.Query("limit"); s != "" {
		if lim, err := strconv.Atoi(s); err == nil && lim > 0 {
			params.Limit = lim
		}
	}
	entries, err := h.base.AuditQuery.Query(c.Context(), params)
	if err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "query_failed")
	}
	return c.JSON(fiber.Map{
		"entries": entries,
		"count":   len(entries),
	})
}

func errJSON(c fiber.Ctx, status int, code string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": dto.NewError(code, auditErrorMessage(code)),
	})
}

func auditErrorMessage(code string) string {
	switch code {
	case "audit_query_unavailable":
		return "Audit query unavailable"
	case "query_failed":
		return "Audit query failed"
	default:
		return code
	}
}
