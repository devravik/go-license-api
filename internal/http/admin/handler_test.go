package admin_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/admin"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/gofiber/fiber/v3"
)

type mockAdminService struct {
	createRespTenant *domain.Tenant
	createRespKey    string
	createErr        error
	revokeErr        error
	rotateKeyErr     error
}

func (m *mockAdminService) CreateTenant(_ context.Context, rps, burst int) (*domain.Tenant, string, error) {
	return m.createRespTenant, m.createRespKey, m.createErr
}
func (m *mockAdminService) RevokeLicense(_ context.Context, tenantID, key string) error {
	return m.revokeErr
}
func (m *mockAdminService) SuspendTenant(_ context.Context, tenantID, reason string) error { return nil }
func (m *mockAdminService) ReinstateTenant(_ context.Context, tenantID string) error       { return nil }
func (m *mockAdminService) DeleteTenant(_ context.Context, tenantID string) error          { return nil }
func (m *mockAdminService) RotateTenantAPIKey(_ context.Context, tenantID string, gracePeriod time.Duration) (string, time.Time, error) {
	return "rotated", time.Now().Add(gracePeriod), m.rotateKeyErr
}
func (m *mockAdminService) UpdateTenantLimits(_ context.Context, tenantID string, rps, burst int) error {
	return nil
}
func (m *mockAdminService) UpdateTenantIPAllowlist(_ context.Context, tenantID string, cidrs []string) error {
	return nil
}

func newAdminTestApp(t *testing.T, adminSvc app.AdminService) *fiber.App {
	t.Helper()
	appCfg := &setup.Config{AppName: "test", AdminKey: "secret"}
	h := handlers.NewHandler(appCfg, nil, nil, adminSvc, nil, nil, nil, nil, nil, nil, nil)
	ah := admin.NewHandler(h)

	app := fiber.New()
	group := app.Group("/admin")
	group.Use(middleware.AdminCIDRGuard(nil))
	group.Use(middleware.AdminKeyGuard(appCfg.AdminKey))
	group.Get("/", ah.Status)
	group.Post("/tenants", ah.CreateTenant)
	group.Post("/licenses/revoke", ah.RevokeLicense)
	group.Post("/tenants/:id/rotate-key", ah.RotateTenantKey)
	return app
}

func TestAdmin_RequiresKey(t *testing.T) {
	app := newAdminTestApp(t, &mockAdminService{})
	req := httptest.NewRequest("GET", "/admin", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestAdmin_CreateTenant_Success(t *testing.T) {
	adminSvc := &mockAdminService{
		createRespTenant: &domain.Tenant{ID: "t-created", APIKey: "gen", RPS: 100, Burst: 200, Status: "active"},
		createRespKey:    "gen",
	}
	app := newAdminTestApp(t, adminSvc)
	req := httptest.NewRequest("POST", "/admin/tenants", bytes.NewBufferString(`{"rps":100,"burst":200}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", "secret")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
}

func TestAdmin_RevokeLicense_Success(t *testing.T) {
	app := newAdminTestApp(t, &mockAdminService{})
	req := httptest.NewRequest("POST", "/admin/licenses/revoke", bytes.NewBufferString(`{"tenant_id":"t1","key":"k1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", "secret")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

