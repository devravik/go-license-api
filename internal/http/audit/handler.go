package audit

import (
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/gofiber/fiber/v3"
	"github.com/devravik/go-license-api/internal/audit"
	"time"
	"strconv"
)

type Handler struct {
	base *handlers.Handler
}

func NewHandler(base *handlers.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Query(c fiber.Ctx) error {
	if h.base.AuditQuery == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "audit_query_unavailable"})
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "query_failed"})
	}
	return c.JSON(fiber.Map{
		"entries": entries,
		"count":   len(entries),
	})
}

