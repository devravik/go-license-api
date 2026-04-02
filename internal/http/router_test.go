package http_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/audit"
	"github.com/devravik/go-license-api/internal/domain"
	httpapi "github.com/devravik/go-license-api/internal/http"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
)

type dummyValidation struct{}

func (d *dummyValidation) Validate(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
	return &domain.ValidationResult{Valid: true}, nil
}

type dummyAdmin struct{}

func (d *dummyAdmin) CreateTenant(ctx context.Context, rps, burst int) (*domain.Tenant, string, error) {
	return &domain.Tenant{ID: "t"}, "k", nil
}
func (d *dummyAdmin) RevokeLicense(ctx context.Context, tenantID, key string) error    { return nil }
func (d *dummyAdmin) SuspendTenant(ctx context.Context, tenantID, reason string) error { return nil }
func (d *dummyAdmin) ReinstateTenant(ctx context.Context, tenantID string) error       { return nil }
func (d *dummyAdmin) DeleteTenant(ctx context.Context, tenantID string) error          { return nil }
func (d *dummyAdmin) RotateTenantAPIKey(ctx context.Context, tenantID string, gracePeriod time.Duration) (string, time.Time, error) {
	return "k2", time.Now().Add(gracePeriod), nil
}
func (d *dummyAdmin) UpdateTenantLimits(ctx context.Context, tenantID string, rps, burst int) error {
	return nil
}
func (d *dummyAdmin) UpdateTenantIPAllowlist(ctx context.Context, tenantID string, cidrs []string) error {
	return nil
}

func newRouterTestApp(t *testing.T) *fiber.App {
	t.Helper()
	cfg := &setup.Config{
		AppName:           "Go License API",
		AppPort:           "8080",
		AdminKey:          "admin",
		AppMode:           "multi",
		AppEnv:            "test",
		JSONEngine:        "std",
		WorkerCount:       1,
		WorkerQueueSize:   8,
		WorkerTimeout:     1500 * time.Millisecond,
		ValidationTimeout: 2 * time.Second,
		ClientTimeout:     3 * time.Second,
		AdminAllowedCIDRs: nil,
	}

	app := fiber.New()
	val := &dummyValidation{}
	admin := &dummyAdmin{}
	l1, _ := cache.NewL1Cache(1000)
	tenantStore := cache.NewTenantStore(l1, nil, time.Hour, time.Minute)
	tenantStore.Set(context.Background(), "t1", "tenant-key", &domain.Tenant{
		ID: "t1", APIKey: "tenant-key", RPS: 100, Burst: 200, Status: "active",
	})
	pool := worker.NewPool(1, 8, val, cfg.WorkerTimeout)
	pool.Start(context.Background())
	rl := middleware.NewRateLimiter()
	httpapi.SetupRoutesV2(app, cfg, val, nil, admin, pool, tenantStore, rl, nil, nil, audit.NewQueryService(nil), nil, nil)
	return app
}

func TestRouter_LicenseValidateExists(t *testing.T) {
	app := newRouterTestApp(t)
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"k","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-API-Key", "tenant-key")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

func TestRouter_AdminProtectedByKey(t *testing.T) {
	app := newRouterTestApp(t)
	req := httptest.NewRequest("GET", "/admin", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}
