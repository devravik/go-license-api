package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/ports"
)

type mockTenantRepo struct {
	createFunc        func(ctx context.Context, t *domain.Tenant) error
	updateStatusFunc  func(ctx context.Context, id, status string) error
	findByIDFunc      func(ctx context.Context, id string) (*domain.Tenant, error)
	findAllFunc       func(ctx context.Context) ([]*domain.Tenant, error)
	rotateAPIKeyFunc  func(ctx context.Context, id, newKey string, gracePeriod time.Duration) error
	updateLimitsFunc  func(ctx context.Context, id string, rps, burst int) error
	updateIPAllowFunc func(ctx context.Context, id string, cidrs []string) error

	lastCreateTenant *domain.Tenant
}

func (m *mockTenantRepo) FindByID(ctx context.Context, id string) (*domain.Tenant, error) {
	return m.findByIDFunc(ctx, id)
}
func (m *mockTenantRepo) FindByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error) {
	return nil, errors.New("not implemented")
}
func (m *mockTenantRepo) FindAll(ctx context.Context) ([]*domain.Tenant, error) {
	return m.findAllFunc(ctx)
}
func (m *mockTenantRepo) Create(ctx context.Context, t *domain.Tenant) error {
	m.lastCreateTenant = t
	return m.createFunc(ctx, t)
}
func (m *mockTenantRepo) UpdateStatus(ctx context.Context, id, status string) error {
	return m.updateStatusFunc(ctx, id, status)
}
func (m *mockTenantRepo) UpdateLimits(ctx context.Context, id string, rps, burst int) error {
	return m.updateLimitsFunc(ctx, id, rps, burst)
}
func (m *mockTenantRepo) UpdateIPAllowlist(ctx context.Context, id string, cidrs []string) error {
	return m.updateIPAllowFunc(ctx, id, cidrs)
}
func (m *mockTenantRepo) RotateAPIKey(ctx context.Context, id, newKey string, gracePeriod time.Duration) error {
	return m.rotateAPIKeyFunc(ctx, id, newKey, gracePeriod)
}

type mockLicenseRepo struct {
	revokeFunc    func(ctx context.Context, tenantID, key string) error
	findByKeyFunc func(ctx context.Context, tenantID, key string) (*domain.License, error)
}

func (m *mockLicenseRepo) FindByKey(ctx context.Context, tenantID, key string) (*domain.License, error) {
	return m.findByKeyFunc(ctx, tenantID, key)
}
func (m *mockLicenseRepo) Create(ctx context.Context, l *domain.License) error {
	return errors.New("not implemented")
}
func (m *mockLicenseRepo) Revoke(ctx context.Context, tenantID, key string) error {
	return m.revokeFunc(ctx, tenantID, key)
}
func (m *mockLicenseRepo) GetRecent(ctx context.Context, limit int) ([]domain.License, error) {
	return nil, errors.New("not implemented")
}
func (m *mockLicenseRepo) Update(ctx context.Context, l *domain.License) error {
	return nil
}
func (m *mockLicenseRepo) ListByTenant(ctx context.Context, tenantID string, limit, offset int) ([]*domain.License, error) {
	return nil, nil
}
func (m *mockLicenseRepo) ListRevocationsSince(ctx context.Context, since *time.Time, limit int) ([]domain.Revocation, error) {
	return []domain.Revocation{}, nil
}

type mockLicenseCache struct {
	setFunc          func(ctx context.Context, tenantID, key string, license *domain.License)
	invalidateTenant func(ctx context.Context, tenantID string) error

	lastSetTenantID string
	lastSetKey      string
	lastSetLicense  *domain.License
}

func (m *mockLicenseCache) Set(ctx context.Context, tenantID, key string, license *domain.License) {
	m.lastSetTenantID = tenantID
	m.lastSetKey = key
	m.lastSetLicense = license
	if m.setFunc != nil {
		m.setFunc(ctx, tenantID, key, license)
	}
}
func (m *mockLicenseCache) InvalidateTenant(ctx context.Context, tenantID string) error {
	return m.invalidateTenant(ctx, tenantID)
}

type mockTenantCache struct {
	setFunc            func(ctx context.Context, tenantID, apiKey string, tenant *domain.Tenant)
	invalidateFunc     func(ctx context.Context, tenantID, apiKey string)
	invalidateByIDFunc func(ctx context.Context, tenantID string)

	lastInvalidateTenantID string
	lastInvalidateAPIKey   string
}

func (m *mockTenantCache) Set(ctx context.Context, tenantID, apiKey string, tenant *domain.Tenant) {
	if m.setFunc != nil {
		m.setFunc(ctx, tenantID, apiKey, tenant)
	}
}
func (m *mockTenantCache) Invalidate(ctx context.Context, tenantID, apiKey string) {
	m.lastInvalidateTenantID = tenantID
	m.lastInvalidateAPIKey = apiKey
	if m.invalidateFunc != nil {
		m.invalidateFunc(ctx, tenantID, apiKey)
	}
}
func (m *mockTenantCache) InvalidateByTenantID(ctx context.Context, tenantID string) {
	m.lastInvalidateTenantID = tenantID
	if m.invalidateByIDFunc != nil {
		m.invalidateByIDFunc(ctx, tenantID)
	}
}

type mockLimiter struct {
	invalidateCount int
	lastTenantID    string
}

func (m *mockLimiter) Invalidate(tenantID string) {
	m.invalidateCount++
	m.lastTenantID = tenantID
}

type mockAuditor struct {
	writeCount int
	lastEntry  *domain.AuditEntry
}

func (m *mockAuditor) Write(ctx context.Context, entry *domain.AuditEntry) {
	m.writeCount++
	m.lastEntry = entry
}
func (m *mockAuditor) Flush() {}

func TestAdminService_CreateTenant_WritesCachesAndInvalidatesLimiter(t *testing.T) {
	ctx := context.Background()

	tenCache := &mockTenantCache{}
	limiter := &mockLimiter{}
	auditor := &mockAuditor{}
	licCache := &mockLicenseCache{
		invalidateTenant: func(ctx context.Context, tenantID string) error { return nil },
	}

	tenantRepo := &mockTenantRepo{
		createFunc:        func(ctx context.Context, t *domain.Tenant) error { return nil },
		updateStatusFunc:  func(ctx context.Context, id, status string) error { return nil },
		findByIDFunc:      func(ctx context.Context, id string) (*domain.Tenant, error) { return nil, nil },
		findAllFunc:       func(ctx context.Context) ([]*domain.Tenant, error) { return []*domain.Tenant{}, nil },
		rotateAPIKeyFunc:  func(ctx context.Context, id, newKey string, gracePeriod time.Duration) error { return nil },
		updateLimitsFunc:  func(ctx context.Context, id string, rps, burst int) error { return nil },
		updateIPAllowFunc: func(ctx context.Context, id string, cidrs []string) error { return nil },
	}

	// No license operations in CreateTenant.
	licenseRepo := &mockLicenseRepo{
		revokeFunc: func(ctx context.Context, tenantID, key string) error { return nil },
		findByKeyFunc: func(ctx context.Context, tenantID, key string) (*domain.License, error) {
			return nil, nil
		},
	}

	var setCalled bool
	tenCache.setFunc = func(ctx context.Context, tenantID, apiKey string, tenant *domain.Tenant) {
		setCalled = true
		if tenantID == "" || apiKey == "" || tenant == nil {
			t.Fatalf("expected tenant cache set with valid values")
		}
	}

	// New signature requires product repo and product cache; tests can pass nils.
	var prodRepo domain.ProductRepository
	var prodCache ports.ProductStore
	svc := app.NewAdminService(licenseRepo, tenantRepo, prodRepo, nil, licCache, tenCache, prodCache, nil, limiter, auditor)
	tenant, apiKey, err := svc.CreateTenant(ctx, 50, 100)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if tenant == nil || apiKey == "" {
		t.Fatalf("expected tenant and apiKey to be returned")
	}
	if !setCalled {
		t.Fatalf("expected tenant cache set to be called")
	}
	if limiter.invalidateCount != 1 || limiter.lastTenantID != tenant.ID {
		t.Fatalf("expected limiter invalidate for tenant %s; got count=%d last=%s", tenant.ID, limiter.invalidateCount, limiter.lastTenantID)
	}
	if auditor.writeCount != 1 || auditor.lastEntry.Event != domain.EventTenantCreated {
		t.Fatalf("expected auditor write %q; got count=%d event=%s", domain.EventTenantCreated, auditor.writeCount, auditor.lastEntry.Event)
	}
}

func TestAdminService_RevokeLicense_UpdatesLicenseCacheAfterRevoke(t *testing.T) {
	ctx := context.Background()

	tenantID, key := "t1", "LIC-1"
	revokeCalled := false

	repoLicense := &mockLicenseRepo{
		revokeFunc: func(ctx context.Context, tenantID, key string) error {
			revokeCalled = true
			return nil
		},
		findByKeyFunc: func(ctx context.Context, tenantID, key string) (*domain.License, error) {
			return &domain.License{TenantID: tenantID, Key: key, Status: "revoked"}, nil
		},
	}

	tenantRepo := &mockTenantRepo{
		createFunc:        func(ctx context.Context, t *domain.Tenant) error { return nil },
		updateStatusFunc:  func(ctx context.Context, id, status string) error { return nil },
		findByIDFunc:      func(ctx context.Context, id string) (*domain.Tenant, error) { return nil, nil },
		findAllFunc:       func(ctx context.Context) ([]*domain.Tenant, error) { return []*domain.Tenant{}, nil },
		rotateAPIKeyFunc:  func(ctx context.Context, id, newKey string, gracePeriod time.Duration) error { return nil },
		updateLimitsFunc:  func(ctx context.Context, id string, rps, burst int) error { return nil },
		updateIPAllowFunc: func(ctx context.Context, id string, cidrs []string) error { return nil },
	}

	licCache := &mockLicenseCache{
		invalidateTenant: func(ctx context.Context, tenantID string) error { return nil },
	}
	tenCache := &mockTenantCache{}
	limiter := &mockLimiter{}
	auditor := &mockAuditor{}

	{
		var prodRepo domain.ProductRepository
		var prodCache ports.ProductStore
		svc := app.NewAdminService(repoLicense, tenantRepo, prodRepo, nil, licCache, tenCache, prodCache, nil, limiter, auditor)
		if err := svc.RevokeLicense(ctx, tenantID, key); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !revokeCalled {
			t.Fatalf("expected revoke to be called")
		}
		if licCache.lastSetTenantID != tenantID || licCache.lastSetKey != key || licCache.lastSetLicense == nil {
			t.Fatalf("expected license cache Set called for tenant=%s key=%s", tenantID, key)
		}
		if aud := auditor.writeCount; aud != 0 {
			t.Fatalf("expected no audit writes on RevokeLicense, got %d", aud)
		}
	}
}

func TestAdminService_SuspendTenant_InvalidatesCaches(t *testing.T) {
	ctx := context.Background()

	tenantID := "t1"
	var updateStatusCalled bool
	var invalidateTenantCalled bool

	tenantRepo := &mockTenantRepo{
		createFunc: func(ctx context.Context, t *domain.Tenant) error { return nil },
		updateStatusFunc: func(ctx context.Context, id, status string) error {
			updateStatusCalled = true
			if id != tenantID || status != "suspended" {
				t.Fatalf("unexpected update status args: id=%s status=%s", id, status)
			}
			return nil
		},
		findByIDFunc: func(ctx context.Context, id string) (*domain.Tenant, error) {
			if id != tenantID {
				t.Fatalf("unexpected FindByID tenant: %s", id)
			}
			return &domain.Tenant{ID: tenantID, APIKey: "tenant-key"}, nil
		},
		findAllFunc:       func(ctx context.Context) ([]*domain.Tenant, error) { return []*domain.Tenant{}, nil },
		rotateAPIKeyFunc:  func(ctx context.Context, id, newKey string, gracePeriod time.Duration) error { return nil },
		updateLimitsFunc:  func(ctx context.Context, id string, rps, burst int) error { return nil },
		updateIPAllowFunc: func(ctx context.Context, id string, cidrs []string) error { return nil },
	}

	licenseRepo := &mockLicenseRepo{
		revokeFunc: func(ctx context.Context, tenantID, key string) error { return nil },
		findByKeyFunc: func(ctx context.Context, tenantID, key string) (*domain.License, error) {
			return nil, nil
		},
	}

	licCache := &mockLicenseCache{
		invalidateTenant: func(ctx context.Context, id string) error {
			invalidateTenantCalled = true
			if id != tenantID {
				t.Fatalf("expected invalidate for tenant %s, got %s", tenantID, id)
			}
			return nil
		},
	}
	tenCache := &mockTenantCache{}
	limiter := &mockLimiter{}
	auditor := &mockAuditor{}

	{
		var prodRepo domain.ProductRepository
		var prodCache ports.ProductStore
		svc := app.NewAdminService(licenseRepo, tenantRepo, prodRepo, nil, licCache, tenCache, prodCache, nil, limiter, auditor)
		if err := svc.SuspendTenant(ctx, tenantID, "fraud"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	}

	if !updateStatusCalled {
		t.Fatalf("expected tenants.UpdateStatus to be called")
	}
	if !invalidateTenantCalled {
		t.Fatalf("expected license cache invalidation to be called")
	}
	if limiter.invalidateCount != 1 || limiter.lastTenantID != tenantID {
		t.Fatalf("expected limiter invalidate for tenant %s; got count=%d last=%s", tenantID, limiter.invalidateCount, limiter.lastTenantID)
	}
	if auditor.writeCount != 1 || auditor.lastEntry.Event != domain.EventTenantSuspended {
		t.Fatalf("expected auditor write %q; got count=%d event=%s", domain.EventTenantSuspended, auditor.writeCount, auditor.lastEntry.Event)
	}
}

func TestEnsureSystemTenant_CreatesWhenEmpty(t *testing.T) {
	ctx := context.Background()
	var created *domain.Tenant
	repo := &mockTenantRepo{
		findAllFunc: func(ctx context.Context) ([]*domain.Tenant, error) {
			return []*domain.Tenant{}, nil
		},
		createFunc: func(ctx context.Context, t *domain.Tenant) error {
			created = t
			return nil
		},
		updateStatusFunc:  func(ctx context.Context, id, status string) error { return nil },
		findByIDFunc:      func(ctx context.Context, id string) (*domain.Tenant, error) { return nil, nil },
		rotateAPIKeyFunc:  func(ctx context.Context, id, newKey string, gracePeriod time.Duration) error { return nil },
		updateLimitsFunc:  func(ctx context.Context, id string, rps, burst int) error { return nil },
		updateIPAllowFunc: func(ctx context.Context, id string, cidrs []string) error { return nil },
	}

	if err := app.EnsureSystemTenant(ctx, repo); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if created == nil {
		t.Fatalf("expected system tenant to be created")
	}
	if len(created.ID) < 4 || created.ID[:4] != "ten_" {
		t.Fatalf("expected generated tenant ID with ten_ prefix, got %q", created.ID)
	}
	if created.Metadata == nil || created.Metadata["is_system"] != true {
		t.Fatalf("expected is_system=true in metadata")
	}
}

func TestAdminService_ResolveTenantID_FallsBackToSystemTenant(t *testing.T) {
	ctx := context.Background()
	tenantRepo := &mockTenantRepo{
		findAllFunc: func(ctx context.Context) ([]*domain.Tenant, error) {
			return []*domain.Tenant{
				{ID: "ten_abc", Metadata: map[string]any{"is_system": true}},
			}, nil
		},
		createFunc:        func(ctx context.Context, t *domain.Tenant) error { return nil },
		updateStatusFunc:  func(ctx context.Context, id, status string) error { return nil },
		findByIDFunc:      func(ctx context.Context, id string) (*domain.Tenant, error) { return nil, nil },
		rotateAPIKeyFunc:  func(ctx context.Context, id, newKey string, gracePeriod time.Duration) error { return nil },
		updateLimitsFunc:  func(ctx context.Context, id string, rps, burst int) error { return nil },
		updateIPAllowFunc: func(ctx context.Context, id string, cidrs []string) error { return nil },
	}
	licCache := &mockLicenseCache{invalidateTenant: func(ctx context.Context, tenantID string) error { return nil }}
	svc := app.NewAdminService(&mockLicenseRepo{
		revokeFunc:    func(ctx context.Context, tenantID, key string) error { return nil },
		findByKeyFunc: func(ctx context.Context, tenantID, key string) (*domain.License, error) { return nil, nil },
	}, tenantRepo, nil, nil, licCache, &mockTenantCache{}, nil, nil, &mockLimiter{}, &mockAuditor{})

	got, err := svc.ResolveTenantID(ctx, "")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "ten_abc" {
		t.Fatalf("expected ten_abc, got %s", got)
	}
}

func TestAdminService_DeleteTenant_ProtectsSystemTenant(t *testing.T) {
	ctx := context.Background()
	tenantID := "ten_system"
	tenantRepo := &mockTenantRepo{
		findByIDFunc: func(ctx context.Context, id string) (*domain.Tenant, error) {
			return &domain.Tenant{ID: tenantID, Metadata: map[string]any{"is_system": true}}, nil
		},
		findAllFunc:       func(ctx context.Context) ([]*domain.Tenant, error) { return []*domain.Tenant{}, nil },
		createFunc:        func(ctx context.Context, t *domain.Tenant) error { return nil },
		updateStatusFunc:  func(ctx context.Context, id, status string) error { return nil },
		rotateAPIKeyFunc:  func(ctx context.Context, id, newKey string, gracePeriod time.Duration) error { return nil },
		updateLimitsFunc:  func(ctx context.Context, id string, rps, burst int) error { return nil },
		updateIPAllowFunc: func(ctx context.Context, id string, cidrs []string) error { return nil },
	}
	licCache := &mockLicenseCache{invalidateTenant: func(ctx context.Context, tenantID string) error { return nil }}
	svc := app.NewAdminService(&mockLicenseRepo{
		revokeFunc:    func(ctx context.Context, tenantID, key string) error { return nil },
		findByKeyFunc: func(ctx context.Context, tenantID, key string) (*domain.License, error) { return nil, nil },
	}, tenantRepo, nil, nil, licCache, &mockTenantCache{}, nil, nil, &mockLimiter{}, &mockAuditor{})

	err := svc.DeleteTenant(ctx, tenantID)
	if err == nil || err.Error() != "system_tenant_protected" {
		t.Fatalf("expected system_tenant_protected, got %v", err)
	}
}
