package middleware

import (
	"context"
	"log"
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
			log.Printf("event=auth_failure reason=missing_tenant_id path=%s", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "missing_tenant_id",
			})
		}
		// Fast format checks to avoid unnecessary cache lookups.
		if len(tenantID) < 8 {
			log.Printf("event=auth_failure reason=invalid_tenant_id_format path=%s", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "invalid_tenant_id_format",
			})
		}

		apiKey := strings.TrimSpace(c.Get("X-API-Key"))
		if apiKey == "" {
			log.Printf("event=auth_failure reason=missing_api_key path=%s tenant_id_len=%d", c.Path(), len(tenantID))
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "missing_api_key",
			})
		}
		if len(apiKey) < 16 {
			log.Printf("event=auth_failure reason=invalid_api_key_format path=%s tenant_id_len=%d", c.Path(), len(tenantID))
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "invalid_api_key_format",
			})
		}

		tenant, err := cache.Get(c.Context(), tenantID, apiKey)
		if err != nil || tenant == nil {
			log.Printf("event=auth_failure reason=invalid_api_key path=%s tenant_id_len=%d api_key_len=%d", c.Path(), len(tenantID), len(apiKey))
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "invalid_api_key",
			})
		}
		if tenant.IsSuspended() {
			log.Printf("event=auth_failure reason=tenant_suspended path=%s tenant_id_len=%d", c.Path(), len(tenantID))
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
