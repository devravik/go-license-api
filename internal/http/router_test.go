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
	security "github.com/devravik/go-license-api/internal/security"
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
func (d *dummyAdmin) ResolveTenantID(ctx context.Context, tenantID string) (string, error) {
	if tenantID == "" {
		return "t-system", nil
	}
	return tenantID, nil
}
func (d *dummyAdmin) CreateLicense(ctx context.Context, l *domain.License) error { return nil }
func (d *dummyAdmin) GetLicense(ctx context.Context, tenantID, key string) (*domain.License, error) {
	return &domain.License{TenantID: tenantID, Key: key}, nil
}
func (d *dummyAdmin) UpdateLicense(ctx context.Context, l *domain.License) error { return nil }
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

// Satisfy new product methods on AdminService.
func (d *dummyAdmin) UpsertProduct(ctx context.Context, p *domain.Product) error { return nil }
func (d *dummyAdmin) DeleteProduct(ctx context.Context, tenantID, productID string) error {
	return nil
}
func (d *dummyAdmin) RestoreProduct(ctx context.Context, tenantID, productID string) error {
	return nil
}
func (d *dummyAdmin) SetProductActive(ctx context.Context, tenantID, productID string, active bool) error {
	return nil
}
func (d *dummyAdmin) CreatePlan(ctx context.Context, p *domain.Plan) error { return nil }
func (d *dummyAdmin) UpdatePlan(ctx context.Context, p *domain.Plan) error { return nil }
func (d *dummyAdmin) GetPlan(ctx context.Context, tenantID, planID string) (*domain.Plan, error) {
	return &domain.Plan{ID: planID, TenantID: tenantID, Name: "pro"}, nil
}
func (d *dummyAdmin) ListPlans(ctx context.Context, tenantID string) ([]*domain.Plan, error) {
	return []*domain.Plan{}, nil
}
func (d *dummyAdmin) DeletePlan(ctx context.Context, tenantID, planID string) error {
	return nil
}
func (d *dummyAdmin) RestorePlan(ctx context.Context, tenantID, planID string) error {
	return nil
}
func (d *dummyAdmin) SetPlanActive(ctx context.Context, tenantID, planID string, active bool) error {
	return nil
}

func newRouterTestApp(t *testing.T) *fiber.App {
	t.Helper()
	cfg := &setup.Config{
		AppName:           "Go License API",
		AppPort:           "8080",
		AdminKey:          "admin",
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
	routerKeyHash := security.HashAPIKey("0123456789abcdef")
	tenantStore.Set(context.Background(), "tenant-0001", routerKeyHash, &domain.Tenant{
		ID: "tenant-0001", APIKey: routerKeyHash, RPS: 100, Burst: 200, Status: "active",
	})
	pool := worker.NewPool(1, 8, val, cfg.WorkerTimeout)
	pool.Start(context.Background())
	rl := middleware.NewRateLimiter()
	httpapi.SetupRoutesV2(app, cfg, val, nil, admin, pool, tenantStore, rl, nil, nil, audit.NewQueryService(nil), nil, nil, nil)
	return app
}

func TestRouter_LicenseValidateExists(t *testing.T) {
	app := newRouterTestApp(t)
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"k","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")
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
