package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/infrastructure/idgen"
	"github.com/devravik/go-license-api/internal/ports"
	security "github.com/devravik/go-license-api/internal/security"
)

type AdminService interface {
	CreateTenant(ctx context.Context, rps, burst int) (*domain.Tenant, string, error)
	ResolveTenantID(ctx context.Context, tenantID string) (string, error)
	CreateLicense(ctx context.Context, l *domain.License) error
	GetLicense(ctx context.Context, tenantID, key string) (*domain.License, error)
	UpdateLicense(ctx context.Context, l *domain.License) error
	RevokeLicense(ctx context.Context, tenantID, key string) error
	SuspendTenant(ctx context.Context, tenantID, reason string) error
	ReinstateTenant(ctx context.Context, tenantID string) error
	DeleteTenant(ctx context.Context, tenantID string) error
	RotateTenantAPIKey(ctx context.Context, tenantID string, gracePeriod time.Duration) (string, time.Time, error)
	UpdateTenantLimits(ctx context.Context, tenantID string, rps, burst int) error
	UpdateTenantIPAllowlist(ctx context.Context, tenantID string, cidrs []string) error
	// Product control-plane methods
	UpsertProduct(ctx context.Context, p *domain.Product) error
	DeleteProduct(ctx context.Context, tenantID, productID string) error
	RestoreProduct(ctx context.Context, tenantID, productID string) error
	SetProductActive(ctx context.Context, tenantID, productID string, active bool) error
	CreatePlan(ctx context.Context, p *domain.Plan) error
	UpdatePlan(ctx context.Context, p *domain.Plan) error
	GetPlan(ctx context.Context, tenantID, planID string) (*domain.Plan, error)
	ListPlans(ctx context.Context, tenantID string) ([]*domain.Plan, error)
	DeletePlan(ctx context.Context, tenantID, planID string) error
	RestorePlan(ctx context.Context, tenantID, planID string) error
	SetPlanActive(ctx context.Context, tenantID, planID string, active bool) error
}

type LicenseCache interface {
	Set(ctx context.Context, tenantID, key string, license *domain.License)
	InvalidateTenant(ctx context.Context, tenantID string) error
}

type TenantCache interface {
	Set(ctx context.Context, tenantID, apiKey string, tenant *domain.Tenant)
	Invalidate(ctx context.Context, tenantID, apiKey string)
	InvalidateByTenantID(ctx context.Context, tenantID string)
}

type adminService struct {
	licenses  domain.LicenseRepository
	tenants   domain.TenantRepository
	products  domain.ProductRepository
	plans     domain.PlanRepository
	licCache  LicenseCache
	tenCache  TenantCache
	prodCache ports.ProductStore
	planCache ports.PlanStore
	limiter   ports.RateLimiter
	auditor   domain.AuditWriter
}

func NewAdminService(licenses domain.LicenseRepository, tenants domain.TenantRepository, products domain.ProductRepository, plans domain.PlanRepository, licCache LicenseCache, tenCache TenantCache, prodCache ports.ProductStore, planCache ports.PlanStore, limiter ports.RateLimiter, auditor domain.AuditWriter) AdminService {
	return &adminService{
		licenses:  licenses,
		tenants:   tenants,
		products:  products,
		plans:     plans,
		licCache:  licCache,
		tenCache:  tenCache,
		prodCache: prodCache,
		planCache: planCache,
		limiter:   limiter,
		auditor:   auditor,
	}
}

func (s *adminService) CreatePlan(ctx context.Context, p *domain.Plan) error {
	if p == nil || p.TenantID == "" || p.Name == "" {
		return errors.New("invalid_plan")
	}
	if p.ID == "" {
		id, err := idgen.NewID("plan")
		if err != nil {
			return fmt.Errorf("generate plan id: %w", err)
		}
		p.ID = id
	}
	if s.plans == nil {
		return errors.New("plan_repo_unavailable")
	}
	if existing, err := s.plans.FindByID(ctx, p.TenantID, p.ID); err == nil && existing != nil {
		return errors.New("plan_already_exists")
	}
	if err := s.plans.Upsert(ctx, p); err != nil {
		return fmt.Errorf("plan create: %w", err)
	}
	if s.planCache != nil {
		s.planCache.Set(ctx, p.TenantID, p.ID, p)
	}
	return nil
}

func (s *adminService) UpdatePlan(ctx context.Context, p *domain.Plan) error {
	if p == nil || p.TenantID == "" || p.ID == "" {
		return errors.New("invalid_plan")
	}
	if s.plans == nil {
		return errors.New("plan_repo_unavailable")
	}
	if existing, err := s.plans.FindByID(ctx, p.TenantID, p.ID); err != nil || existing == nil {
		return errors.New("plan_not_found")
	}
	if err := s.plans.Upsert(ctx, p); err != nil {
		return fmt.Errorf("plan update: %w", err)
	}
	if s.planCache != nil {
		s.planCache.Set(ctx, p.TenantID, p.ID, p)
	}
	return nil
}

func (s *adminService) GetPlan(ctx context.Context, tenantID, planID string) (*domain.Plan, error) {
	if tenantID == "" || planID == "" {
		return nil, errors.New("invalid_request")
	}
	if s.plans == nil {
		return nil, errors.New("plan_repo_unavailable")
	}
	if s.planCache != nil {
		if p, err := s.planCache.Get(ctx, tenantID, planID); err == nil && p != nil {
			return p, nil
		}
	}
	p, err := s.plans.FindByID(ctx, tenantID, planID)
	if err == nil && p != nil && s.planCache != nil {
		s.planCache.Set(ctx, tenantID, planID, p)
	}
	return p, err
}

func (s *adminService) ListPlans(ctx context.Context, tenantID string) ([]*domain.Plan, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id_required")
	}
	if s.plans == nil {
		return nil, errors.New("plan_repo_unavailable")
	}
	return s.plans.ListByTenant(ctx, tenantID)
}

func (s *adminService) DeletePlan(ctx context.Context, tenantID, planID string) error {
	if tenantID == "" || planID == "" {
		return errors.New("invalid_request")
	}
	if s.plans == nil {
		return errors.New("plan_repo_unavailable")
	}
	if err := s.plans.Delete(ctx, tenantID, planID); err != nil {
		return fmt.Errorf("plan delete: %w", err)
	}
	if s.planCache != nil {
		s.planCache.Invalidate(ctx, tenantID, planID)
	}
	return nil
}

func (s *adminService) SetPlanActive(ctx context.Context, tenantID, planID string, active bool) error {
	if tenantID == "" || planID == "" {
		return errors.New("invalid_request")
	}
	if s.plans == nil {
		return errors.New("plan_repo_unavailable")
	}
	if err := s.plans.SetActive(ctx, tenantID, planID, active); err != nil {
		return fmt.Errorf("plan set_active: %w", err)
	}
	if s.planCache != nil {
		s.planCache.Invalidate(ctx, tenantID, planID)
	}
	return nil
}

func (s *adminService) RestorePlan(ctx context.Context, tenantID, planID string) error {
	if tenantID == "" || planID == "" {
		return errors.New("invalid_request")
	}
	if s.plans == nil {
		return errors.New("plan_repo_unavailable")
	}
	if err := s.plans.Restore(ctx, tenantID, planID); err != nil {
		return fmt.Errorf("plan restore: %w", err)
	}
	if s.planCache != nil {
		p, err := s.plans.FindByID(ctx, tenantID, planID)
		if err != nil {
			return fmt.Errorf("plan restore fetch: %w", err)
		}
		s.planCache.Set(ctx, tenantID, planID, p)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      "plan_restored",
		ResourceID: planID,
		Outcome:    "success",
	})
	return nil
}

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *adminService) CreateLicense(ctx context.Context, l *domain.License) error {
	if l == nil || l.TenantID == "" || l.Key == "" {
		return errors.New("invalid_license")
	}
	if err := l.ValidateHardRules(); err != nil {
		return err
	}
	if err := s.licenses.Create(ctx, l); err != nil {
		return fmt.Errorf("create license: %w", err)
	}
	if l.Type == "plan" && s.plans != nil && l.PlanID != nil {
		var p *domain.Plan
		if s.planCache != nil {
			p, _ = s.planCache.Get(ctx, l.TenantID, *l.PlanID)
		}
		if p == nil {
			p, _ = s.plans.FindByID(ctx, l.TenantID, *l.PlanID)
			if p != nil && s.planCache != nil {
				s.planCache.Set(ctx, l.TenantID, *l.PlanID, p)
			}
		}
		if p != nil {
			l.ResolveFinalFeatures(p, time.Now())
			if p.ProductID != nil {
				l.ProductID = p.ProductID
				l.Product = *p.ProductID
			}
		}
	} else {
		l.ResolveFinalFeatures(nil, time.Now())
	}
	s.licCache.Set(ctx, l.TenantID, l.Key, l)
	return nil
}

func (s *adminService) GetLicense(ctx context.Context, tenantID, key string) (*domain.License, error) {
	return s.licenses.FindByKey(ctx, tenantID, key)
}

func (s *adminService) UpdateLicense(ctx context.Context, l *domain.License) error {
	if l == nil || l.TenantID == "" || l.Key == "" {
		return errors.New("invalid_license")
	}
	if err := l.ValidateHardRules(); err != nil {
		return err
	}
	if err := s.licenses.Update(ctx, l); err != nil {
		return fmt.Errorf("update license: %w", err)
	}
	if l.Type == "plan" && s.plans != nil && l.PlanID != nil {
		var p *domain.Plan
		if s.planCache != nil {
			p, _ = s.planCache.Get(ctx, l.TenantID, *l.PlanID)
		}
		if p == nil {
			p, _ = s.plans.FindByID(ctx, l.TenantID, *l.PlanID)
			if p != nil && s.planCache != nil {
				s.planCache.Set(ctx, l.TenantID, *l.PlanID, p)
			}
		}
		if p != nil {
			l.ResolveFinalFeatures(p, time.Now())
			if p.ProductID != nil {
				l.ProductID = p.ProductID
				l.Product = *p.ProductID
			}
		}
	} else {
		l.ResolveFinalFeatures(nil, time.Now())
	}
	s.licCache.Set(ctx, l.TenantID, l.Key, l)
	return nil
}

func (s *adminService) CreateTenant(ctx context.Context, rps, burst int) (*domain.Tenant, string, error) {
	if rps <= 0 || burst <= 0 {
		return nil, "", errors.New("invalid_limits")
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}

	genID, err := idgen.NewID("ten")
	if err != nil {
		return nil, "", fmt.Errorf("generate tenant id: %w", err)
	}
	slug := "t-" + genID
	name := "Tenant " + genID[:10]

	tenant := &domain.Tenant{
		ID:     genID,
		APIKey: security.HashAPIKey(apiKey), // store hash; caller receives plaintext
		RPS:    rps,
		Burst:  burst,
		Status: "active",
		Name:   name,
		Slug:   slug,
	}
	if err := s.tenants.Create(ctx, tenant); err != nil {
		return nil, "", fmt.Errorf("create tenant: %w", err)
	}

	s.tenCache.Set(ctx, tenant.ID, tenant.APIKey, tenant)
	s.limiter.Invalidate(tenant.ID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenant.ID,
		Event:      domain.EventTenantCreated,
		ResourceID: tenant.ID,
		Outcome:    "success",
	})
	return tenant, apiKey, nil
}

func (s *adminService) ResolveTenantID(ctx context.Context, tenantID string) (string, error) {
	if tenantID != "" {
		return tenantID, nil
	}
	systemTenant, err := s.findSystemTenant(ctx)
	if err != nil {
		return "", err
	}
	return systemTenant.ID, nil
}

func (s *adminService) RevokeLicense(ctx context.Context, tenantID, key string) error {
	if err := s.licenses.Revoke(ctx, tenantID, key); err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	lic, err := s.licenses.FindByKey(ctx, tenantID, key)
	if err != nil {
		return fmt.Errorf("fetch revoked license: %w", err)
	}
	s.licCache.Set(ctx, tenantID, key, lic)
	return nil
}

func (s *adminService) SuspendTenant(ctx context.Context, tenantID, reason string) error {
	t, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("find tenant: %w", err)
	}
	if isSystemTenant(t) {
		return errors.New("system_tenant_protected")
	}
	if err := s.tenants.UpdateStatus(ctx, tenantID, "suspended"); err != nil {
		return fmt.Errorf("suspend tenant: %w", err)
	}
	// Invalidate all licenses for tenant in cache layers.
	if err := s.licCache.InvalidateTenant(ctx, tenantID); err != nil {
		return err
	}
	// Invalidate tenant API keys from cache (best effort).
	t, err = s.tenants.FindByID(ctx, tenantID)
	if err == nil && t != nil {
		s.tenCache.InvalidateByTenantID(ctx, tenantID)
		s.tenCache.Invalidate(ctx, tenantID, t.APIKey)
		s.limiter.Invalidate(tenantID)
	}
	_ = reason // reserved for future audit logging
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantSuspended,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) ReinstateTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, "active"); err != nil {
		return fmt.Errorf("reinstate tenant: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantReinstated,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) DeleteTenant(ctx context.Context, tenantID string) error {
	t, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("find tenant: %w", err)
	}
	if isSystemTenant(t) {
		return errors.New("system_tenant_protected")
	}
	if err := s.tenants.UpdateStatus(ctx, tenantID, "deleted"); err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantDeleted,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) RotateTenantAPIKey(ctx context.Context, tenantID string, gracePeriod time.Duration) (string, time.Time, error) {
	newKey, err := generateAPIKey()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate api key: %w", err)
	}

	newKeyHash := security.HashAPIKey(newKey)
	if err := s.tenants.RotateAPIKey(ctx, tenantID, newKeyHash, gracePeriod); err != nil {
		return "", time.Time{}, fmt.Errorf("rotate api key: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantKeyRotated,
		ResourceID: tenantID,
		Outcome:    "success",
	})

	return newKey, time.Now().Add(gracePeriod), nil
}

func (s *adminService) UpdateTenantLimits(ctx context.Context, tenantID string, rps, burst int) error {
	if rps <= 0 || burst <= 0 {
		return errors.New("invalid_limits")
	}
	if err := s.tenants.UpdateLimits(ctx, tenantID, rps, burst); err != nil {
		return fmt.Errorf("update tenant limits: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantLimitsUpdated,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) UpdateTenantIPAllowlist(ctx context.Context, tenantID string, cidrs []string) error {
	if err := s.tenants.UpdateIPAllowlist(ctx, tenantID, cidrs); err != nil {
		return fmt.Errorf("update tenant ip allowlist: %w", err)
	}
	s.tenCache.InvalidateByTenantID(ctx, tenantID)
	s.limiter.Invalidate(tenantID)
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventTenantIPAllowlistUpdated,
		ResourceID: tenantID,
		Outcome:    "success",
	})
	return nil
}

// UpsertProduct writes through to cache to ensure immediate runtime availability.
func (s *adminService) UpsertProduct(ctx context.Context, p *domain.Product) error {
	if p == nil || p.TenantID == "" || p.Code == "" {
		return errors.New("invalid_product")
	}
	if err := s.products.Upsert(ctx, p); err != nil {
		return fmt.Errorf("product upsert: %w", err)
	}
	if s.prodCache != nil {
		s.prodCache.Set(ctx, p.TenantID, p.Code, p)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   p.TenantID,
		Event:      "product_upserted",
		ResourceID: p.ID,
		Outcome:    "success",
	})
	return nil
}

// DeleteProduct invalidates from cache; DB is source of truth for persistence.
func (s *adminService) DeleteProduct(ctx context.Context, tenantID, productID string) error {
	if tenantID == "" || productID == "" {
		return errors.New("invalid_request")
	}
	// Need code for cache invalidation: fetch by ID (control plane can hit DB).
	p, err := s.products.FindByID(ctx, tenantID, productID)
	if err != nil {
		return fmt.Errorf("product find: %w", err)
	}
	if err := s.products.Delete(ctx, tenantID, productID); err != nil {
		return fmt.Errorf("product delete: %w", err)
	}
	if s.prodCache != nil {
		s.prodCache.Invalidate(ctx, tenantID, p.Code)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      "product_deleted",
		ResourceID: productID,
		Outcome:    "success",
	})
	return nil
}

// SetProductActive toggles active flag and updates cache entry.
func (s *adminService) SetProductActive(ctx context.Context, tenantID, productID string, active bool) error {
	if tenantID == "" || productID == "" {
		return errors.New("invalid_request")
	}
	// fetch for code and current fields
	p, err := s.products.FindByID(ctx, tenantID, productID)
	if err != nil {
		return fmt.Errorf("product find: %w", err)
	}
	if err := s.products.SetActive(ctx, tenantID, productID, active); err != nil {
		return fmt.Errorf("product set_active: %w", err)
	}
	// reflect new flag and write-through
	p.IsActive = active
	if s.prodCache != nil {
		s.prodCache.Set(ctx, tenantID, p.Code, p)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      "product_set_active",
		ResourceID: productID,
		Outcome:    "success",
	})
	return nil
}

func (s *adminService) RestoreProduct(ctx context.Context, tenantID, productID string) error {
	if tenantID == "" || productID == "" {
		return errors.New("invalid_request")
	}
	if s.products == nil {
		return errors.New("product_repo_unavailable")
	}
	if err := s.products.Restore(ctx, tenantID, productID); err != nil {
		return fmt.Errorf("product restore: %w", err)
	}
	p, err := s.products.FindByID(ctx, tenantID, productID)
	if err != nil {
		return fmt.Errorf("product find after restore: %w", err)
	}
	if s.prodCache != nil {
		s.prodCache.Set(ctx, tenantID, p.Code, p)
	}
	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      "product_restored",
		ResourceID: productID,
		Outcome:    "success",
	})
	return nil
}

// UpdateTenantProfile is intentionally not part of the AdminService interface to keep
// backward compatibility for existing mocks and tests. Handlers can detect support via
// type assertion on this method set.
func (s *adminService) UpdateTenantProfile(ctx context.Context, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error {
	type tenantProfileRepo interface {
		UpdateProfile(ctx context.Context, id string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	}
	type tenantFinder interface {
		FindByID(ctx context.Context, id string) (*domain.Tenant, error)
	}
	tr, ok := s.tenants.(tenantProfileRepo)
	if !ok {
		return errors.New("update_profile_not_supported")
	}
	if err := tr.UpdateProfile(ctx, tenantID, name, slug, email, company, plan, maxLicenses, metadata); err != nil {
		return fmt.Errorf("update tenant profile: %w", err)
	}
	if tf, ok := s.tenants.(tenantFinder); ok {
		if t, err := tf.FindByID(ctx, tenantID); err == nil && t != nil {
			s.tenCache.Set(ctx, t.ID, t.APIKey, t)
		}
	}
	s.limiter.Invalidate(tenantID)
	return nil
}

func (s *adminService) findSystemTenant(ctx context.Context) (*domain.Tenant, error) {
	tenants, err := s.tenants.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	for _, t := range tenants {
		if isSystemTenant(t) {
			return t, nil
		}
	}
	return nil, errors.New("system_tenant_not_found")
}

func isSystemTenant(t *domain.Tenant) bool {
	if t == nil || t.Metadata == nil {
		return false
	}
	v, ok := t.Metadata["is_system"]
	if !ok {
		return false
	}
	flag, ok := v.(bool)
	return ok && flag
}
