package middleware_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/gofiber/fiber/v3"
)

type mockTenantCache struct {
	tenant *domain.Tenant
	err    error
}

func (m *mockTenantCache) Get(ctx context.Context, tenantID, apiKey string) (*domain.Tenant, error) {
	return m.tenant, m.err
}

func TestTenantAuth_MissingHeaders(t *testing.T) {
	app := fiber.New()
	cache := &mockTenantCache{}
	app.Post("/licenses/validate", middleware.TenantAuth("multi", nil, cache), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestTenantAuth_InvalidAPIKey(t *testing.T) {
	app := fiber.New()
	cache := &mockTenantCache{tenant: nil, err: domain.ErrInvalidTenant}
	app.Post("/licenses/validate", middleware.TenantAuth("multi", nil, cache), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-API-Key", "bad")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestTenantAuth_SuspendedTenant(t *testing.T) {
	app := fiber.New()
	cache := &mockTenantCache{tenant: &domain.Tenant{ID: "t1", APIKey: "k", Status: "suspended"}}
	app.Post("/licenses/validate", middleware.TenantAuth("multi", nil, cache), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-API-Key", "k")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", res.StatusCode)
	}
}

func TestTenantAuth_ValidSetsContext(t *testing.T) {
	app := fiber.New()
	cache := &mockTenantCache{tenant: &domain.Tenant{ID: "t1", APIKey: "k", Status: "active"}}
	app.Post("/licenses/validate",
		middleware.TenantAuth("multi", nil, cache),
		func(c fiber.Ctx) error {
			if c.Locals("tenant_id") != "t1" {
				return c.SendStatus(500)
			}
			if c.Locals("api_key") != "k" {
				return c.SendStatus(500)
			}
			return c.SendStatus(200)
		},
	)
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-API-Key", "k")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

