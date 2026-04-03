package middleware

import (
	"context"
	"log"
	"strings"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
)

const tenantCtxKey = "tenant"
const tenantIDCtxKey = "tenant_id"
const apiKeyCtxKey = "api_key"

type ErrorResponse struct {
	Success bool          `json:"success"`
	Error   *dto.APIError `json:"error"`
}

func errorResponse(code, message string) ErrorResponse {
	return ErrorResponse{
		Success: false,
		Error:   dto.NewError(code, message),
	}
}

type TenantCache interface {
	GetByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error)
}

func TenantAuth(mode string, defaultTenant *domain.Tenant, cache TenantCache) fiber.Handler {
	return func(c fiber.Ctx) error {
		if mode == "single" && defaultTenant != nil {
			c.Locals(tenantCtxKey, defaultTenant)
			c.Locals(tenantIDCtxKey, defaultTenant.ID)
			c.Locals(apiKeyCtxKey, defaultTenant.APIKey)
			return c.Next()
		}

		apiKey := strings.TrimSpace(c.Get("X-API-Key"))
		if apiKey == "" {
			log.Printf("event=auth_failure reason=missing_api_key path=%s", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("missing_api_key", "Missing API key"))
		}
		if len(apiKey) < 16 {
			log.Printf("event=auth_failure reason=invalid_api_key_format path=%s", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("invalid_api_key_format", "Invalid API key format"))
		}

		tenant, err := cache.GetByAPIKey(c.Context(), apiKey)
		if err != nil || tenant == nil {
			log.Printf("event=auth_failure reason=invalid_api_key path=%s api_key_len=%d", c.Path(), len(apiKey))
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("invalid_api_key", "Invalid API key"))
		}
		if tenant.IsSuspended() {
			log.Printf("event=auth_failure reason=tenant_suspended path=%s tenant_id=%s", c.Path(), tenant.ID)
			return c.Status(fiber.StatusForbidden).JSON(errorResponse("tenant_suspended", "Tenant suspended"))
		}

		c.Locals(tenantCtxKey, tenant)
		c.Locals(tenantIDCtxKey, tenant.ID)
		c.Locals(apiKeyCtxKey, apiKey)
		return c.Next()
	}
}

func TenantFromCtx(c fiber.Ctx) *domain.Tenant {
	t, _ := c.Locals(tenantCtxKey).(*domain.Tenant)
	return t
}
