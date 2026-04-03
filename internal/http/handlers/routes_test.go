package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	httpapi "github.com/devravik/go-license-api/internal/http"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	security "github.com/devravik/go-license-api/internal/security"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
)

type mockValidationService struct {
	lastTenantID string
	lastAPIKey   string
	lastKey      string
	lastProd     string
	result       *domain.ValidationResult
	err          error
	delay        time.Duration
}

func (m *mockValidationService) Validate(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.lastTenantID = tenantID
	m.lastAPIKey = apiKey
	m.lastKey = key
	m.lastProd = product
	if m.result == nil {
		return &domain.ValidationResult{Valid: true}, m.err
	}
	return m.result, m.err
}

type mockAdminService struct {
	revokeErr            error
	suspendErr           error
	reinstateErr         error
	deleteErr            error
	rotateErr            error
	createErr            error
	updateLimitsErr      error
	updateIPAllowlistErr error
}

func (m *mockAdminService) CreateTenant(_ context.Context, rps, burst int) (*domain.Tenant, string, error) {
	if m.createErr != nil {
		return nil, "", m.createErr
	}
	return &domain.Tenant{
		ID:     "t-created",
		APIKey: "generated-key",
		RPS:    rps,
		Burst:  burst,
		Status: "active",
	}, "generated-key", nil
}
func (m *mockAdminService) RevokeLicense(_ context.Context, tenantID, key string) error {
	return m.revokeErr
}
func (m *mockAdminService) SuspendTenant(_ context.Context, tenantID, reason string) error {
	return m.suspendErr
}
func (m *mockAdminService) ReinstateTenant(_ context.Context, tenantID string) error {
	return m.reinstateErr
}
func (m *mockAdminService) DeleteTenant(_ context.Context, tenantID string) error {
	return m.deleteErr
}
func (m *mockAdminService) RotateTenantAPIKey(_ context.Context, tenantID string, gracePeriod time.Duration) (string, time.Time, error) {
	if m.rotateErr != nil {
		return "", time.Time{}, m.rotateErr
	}
	return "new-generated-key", time.Now().Add(gracePeriod), nil
}
func (m *mockAdminService) UpdateTenantLimits(_ context.Context, tenantID string, rps, burst int) error {
	return m.updateLimitsErr
}
func (m *mockAdminService) UpdateTenantIPAllowlist(_ context.Context, tenantID string, cidrs []string) error {
	return m.updateIPAllowlistErr
}

// Satisfy new product methods on AdminService interface for router tests.
func (m *mockAdminService) UpsertProduct(_ context.Context, p *domain.Product) error { return nil }
func (m *mockAdminService) DeleteProduct(_ context.Context, tenantID, productID string) error {
	return nil
}
func (m *mockAdminService) SetProductActive(_ context.Context, tenantID, productID string, active bool) error {
	return nil
}

func newTestApp(t *testing.T, val *mockValidationService, admin *mockAdminService) *fiber.App {
	t.Helper()
	cfg := &setup.Config{
		AppName:           "Go License API",
		AppPort:           "8080",
		AdminKey:          "test-admin-key",
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
	return newTestAppWithConfig(t, cfg, val, admin)
}

func newTestAppWithConfig(t *testing.T, cfg *setup.Config, val *mockValidationService, admin *mockAdminService) *fiber.App {
	t.Helper()
	app := fiber.New()
	l1, err := cache.NewL1Cache(1000)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}
	tenantStore := cache.NewTenantStore(l1, nil, time.Hour, time.Minute)
	keyHash := security.HashAPIKey("0123456789abcdef")
	tenantStore.Set(context.Background(), "tenant-0001", keyHash, &domain.Tenant{
		ID:     "tenant-0001",
		APIKey: keyHash, // domain field holds the hash, never plaintext
		RPS:    100,
		Burst:  200,
		Status: "active",
	})

	pool := worker.NewPool(1, 8, val, cfg.WorkerTimeout)
	pool.Start(context.Background())
	httpapi.SetupRoutes(app, cfg, val, nil, admin, pool, tenantStore, middleware.NewRateLimiter())
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
			Meta:  &domain.ValidationMeta{Plan: "pro"},
		},
	}
	app := newTestApp(t, val, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if val.lastTenantID != "tenant-0001" || val.lastAPIKey != security.HashAPIKey("0123456789abcdef") || val.lastKey != "abc-123" || val.lastProd != "pro" {
		t.Fatalf("validation service called with unexpected args: %+v", val)
	}
}

func TestValidateRoute_KeyRequired(t *testing.T) {
	app := newTestApp(t, &mockValidationService{}, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestValidateRoute_ValidationErrorPayload(t *testing.T) {
	val := &mockValidationService{
		result: &domain.ValidationResult{
			Valid: false,
			Error: "license_expired",
		},
	}
	app := newTestApp(t, val, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var out dto.LicenseValidationResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Valid {
		t.Fatalf("expected valid=false")
	}
	if out.Error == nil || out.Error.Code != "license_expired" {
		t.Fatalf("expected error code license_expired, got %+v", out.Error)
	}
}

func TestValidateRoute_InternalValidationError500(t *testing.T) {
	val := &mockValidationService{
		err: errors.New("boom"),
		// result intentionally left nil to test handler path when validation returns a non-nil result + error.
	}
	app := newTestApp(t, val, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", res.StatusCode)
	}
}

func TestValidateRoute_InvalidJSON400(t *testing.T) {
	app := newTestApp(t, &mockValidationService{}, &mockAdminService{})

	// Malformed JSON => fiber Bind error => invalid_request_body.
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123",`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")

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
		status int
	}{
		{"POST", "/admin/tenants", `{"rps":100,"burst":200}`, 201},
		{"POST", "/admin/licenses/revoke", `{"tenant_id":"tenant-0001","key":"k1"}`, 200},
		{"POST", "/admin/tenants/t1/suspend", `{"reason":"fraud"}`, 200},
		{"POST", "/admin/tenants/t1/reinstate", `{}`, 200},
		{"POST", "/admin/tenants/t1/rotate-key", `{"grace_minutes":60}`, 200},
		{"PATCH", "/admin/tenants/t1/limits", `{"rps":50,"burst":100}`, 200},
		{"POST", "/admin/tenants/t1/ip-allowlist", `{"cidrs":["127.0.0.1/32"]}`, 200},
		{"DELETE", "/admin/tenants/t1", ``, 204},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Key", "test-admin-key")
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", tc.method, tc.path, err)
		}
		if res.StatusCode != tc.status {
			t.Fatalf("%s %s expected %d, got %d", tc.method, tc.path, tc.status, res.StatusCode)
		}
	}
}

func TestValidateRoute_Timeout504(t *testing.T) {
	cfg := &setup.Config{
		AppName:           "Go License API",
		AppPort:           "8080",
		AdminKey:          "test-admin-key",
		AppMode:           "multi",
		AppEnv:            "test",
		JSONEngine:        "std",
		WorkerCount:       1,
		WorkerQueueSize:   8,
		WorkerTimeout:     2 * time.Second,
		ValidationTimeout: 25 * time.Millisecond,
		ClientTimeout:     2 * time.Second,
		AdminAllowedCIDRs: nil,
	}
	val := &mockValidationService{delay: 200 * time.Millisecond}
	app := newTestAppWithConfig(t, cfg, val, &mockAdminService{})

	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate timeout request failed: %v", err)
	}
	if res.StatusCode != 504 {
		t.Fatalf("expected 504, got %d", res.StatusCode)
	}
}

// TestValidateRoute_BypassesPool verifies that validation calls the service
// directly and succeeds even when the worker pool queue is full.
func TestValidateRoute_BypassesPool(t *testing.T) {
	cfg := &setup.Config{
		AppName:           "Go License API",
		AppPort:           "8080",
		AdminKey:          "test-admin-key",
		AppMode:           "multi",
		AppEnv:            "test",
		JSONEngine:        "std",
		WorkerCount:       0,
		WorkerQueueSize:   1,
		WorkerTimeout:     2 * time.Second,
		ValidationTimeout: 200 * time.Millisecond,
		ClientTimeout:     2 * time.Second,
		AdminAllowedCIDRs: nil,
	}
	val := &mockValidationService{
		result: &domain.ValidationResult{Valid: true, Meta: &domain.ValidationMeta{Plan: "pro"}},
	}
	admin := &mockAdminService{}
	app := fiber.New()
	l1, err := cache.NewL1Cache(1000)
	if err != nil {
		t.Fatalf("new l1 cache: %v", err)
	}
	tenantStore := cache.NewTenantStore(l1, nil, time.Hour, time.Minute)
	bypassKeyHash := security.HashAPIKey("0123456789abcdef")
	tenantStore.Set(context.Background(), "tenant-0001", bypassKeyHash, &domain.Tenant{
		ID:     "tenant-0001",
		APIKey: bypassKeyHash,
		RPS:    100,
		Burst:  200,
		Status: "active",
	})
	pool := worker.NewPool(cfg.WorkerCount, cfg.WorkerQueueSize, val, cfg.WorkerTimeout)
	pool.Start(context.Background())
	// Fill the single-slot queue; with zero workers it remains full.
	ok := pool.Enqueue(&worker.ValidateJob{
		TenantID:   "tenant-0001",
		APIKey:     bypassKeyHash,
		LicenseKey: "abc-123",
		Product:    "pro",
		Ctx:        context.Background(),
		ResultCh:   make(chan worker.Result, 1),
	})
	if !ok {
		t.Fatal("expected prefill enqueue to succeed")
	}
	httpapi.SetupRoutes(app, cfg, val, nil, admin, pool, tenantStore, middleware.NewRateLimiter())

	// Validation should succeed (200) because the handler calls the service
	// directly instead of routing through the saturated worker pool.
	req := httptest.NewRequest("POST", "/licenses/validate", bytes.NewBufferString(`{"key":"abc-123","product":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-0001")
	req.Header.Set("X-API-Key", "0123456789abcdef")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200 (pool bypassed), got %d", res.StatusCode)
	}
}
