package tests

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/domain"
	httpapi "github.com/devravik/go-license-api/internal/http"
	"github.com/gofiber/fiber/v3"
)

type mockValidationService struct {
	lastAPIKey string
	lastKey    string
	lastProd   string
	result     *domain.ValidationResult
	err        error
}

func (m *mockValidationService) Validate(_ context.Context, apiKey, key, product string) (*domain.ValidationResult, error) {
	m.lastAPIKey = apiKey
	m.lastKey = key
	m.lastProd = product
	if m.result == nil {
		return &domain.ValidationResult{Valid: true}, m.err
	}
	return m.result, m.err
}

type mockAdminService struct {
	revokeErr  error
	suspendErr error
	rotateErr  error
}

func (m *mockAdminService) RevokeLicense(_ context.Context, tenantID, key string) error {
	return m.revokeErr
}
func (m *mockAdminService) SuspendTenant(_ context.Context, tenantID, reason string) error {
	return m.suspendErr
}
func (m *mockAdminService) RotateTenantAPIKey(_ context.Context, tenantID, newKey string, gracePeriod time.Duration) error {
	return m.rotateErr
}

func newTestApp(t *testing.T, val *mockValidationService, admin *mockAdminService) *fiber.App {
	t.Helper()
	cfg := &configs.Config{
		AppName:    "Go License API",
		AppPort:    "8080",
		AdminKey:   "test-admin-key",
		AppMode:    "single",
		AppEnv:     "test",
		JSONEngine: "std",
	}
	// middleware.Auth uses configs.Load() (env-backed)
	t.Setenv("ADMIN_API_KEY", cfg.AdminKey)

	app := fiber.New()
	httpapi.SetupRoutes(app, cfg, val, admin)
	return app
}

func TestPublicRoutes(t *testing.T) {
	app := newTestApp(t, &mockValidationService{}, &mockAdminService{})

	req := httptest.NewRequest("GET", "/", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("home request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	req = httptest.NewRequest("GET", "/health", nil)
	res, err = app.Test(req)
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

func TestValidateRoute_RequiresTenantAPIKey(t *testing.T) {
	val := &mockValidationService{}
	app := newTestApp(t, val, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestValidateRoute_Success(t *testing.T) {
	val := &mockValidationService{
		result: &domain.ValidationResult{
			Valid: true,
			Meta:  map[string]any{"plan": "pro"},
		},
	}
	app := newTestApp(t, val, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "tenant-key")

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if val.lastAPIKey != "tenant-key" || val.lastKey != "abc-123" || val.lastProd != "pro" {
		t.Fatalf("validation service called with unexpected args: %+v", val)
	}
}

func TestValidateRoute_KeyRequired(t *testing.T) {
	app := newTestApp(t, &mockValidationService{}, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "tenant-key")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestAdminRoutes_RequireAdminKey(t *testing.T) {
	app := newTestApp(t, &mockValidationService{}, &mockAdminService{})

	req := httptest.NewRequest("GET", "/admin", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("admin request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestAdminStatus_Success(t *testing.T) {
	app := newTestApp(t, &mockValidationService{}, &mockAdminService{})

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("X-Admin-Key", "test-admin-key")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("admin status request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

func TestAdminMutations_Success(t *testing.T) {
	app := newTestApp(t, &mockValidationService{}, &mockAdminService{})

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{"POST", "/admin/licenses/revoke", `{"tenant_id":"t1","key":"k1"}`},
		{"POST", "/admin/tenants/t1/suspend", `{"reason":"fraud"}`},
		{"POST", "/admin/tenants/t1/rotate_key", `{"new_key":"new-abc"}`},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Key", "test-admin-key")
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", tc.method, tc.path, err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("%s %s expected 200, got %d", tc.method, tc.path, res.StatusCode)
		}
	}
}

