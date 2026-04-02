package middleware

import (
	"context"
	"strings"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/gofiber/fiber/v3"
)

const tenantCtxKey = "tenant"
const tenantIDCtxKey = "tenant_id"
const apiKeyCtxKey = "api_key"

type ErrorResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error"`
}

type TenantCache interface {
	Get(ctx context.Context, tenantID, apiKey string) (*domain.Tenant, error)
}

func TenantAuth(mode string, defaultTenant *domain.Tenant, cache TenantCache) fiber.Handler {
	return func(c fiber.Ctx) error {
		if mode == "single" && defaultTenant != nil {
			c.Locals(tenantCtxKey, defaultTenant)
			c.Locals(tenantIDCtxKey, defaultTenant.ID)
			c.Locals(apiKeyCtxKey, defaultTenant.APIKey)
			return c.Next()
		}

		tenantID := strings.TrimSpace(c.Get("X-Tenant-ID"))
		if tenantID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "missing_tenant_id",
			})
		}

		apiKey := strings.TrimSpace(c.Get("X-API-Key"))
		if apiKey == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "missing_api_key",
			})
		}

		tenant, err := cache.Get(c.Context(), tenantID, apiKey)
		if err != nil || tenant == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "invalid_api_key",
			})
		}
		if tenant.IsSuspended() {
			return c.Status(fiber.StatusForbidden).JSON(ErrorResponse{
				Valid: false,
				Error: "tenant_suspended",
			})
		}

		c.Locals(tenantCtxKey, tenant)
		c.Locals(tenantIDCtxKey, tenantID)
		c.Locals(apiKeyCtxKey, apiKey)
		return c.Next()
	}
}

func TenantFromCtx(c fiber.Ctx) *domain.Tenant {
	t, _ := c.Locals(tenantCtxKey).(*domain.Tenant)
	return t
}
