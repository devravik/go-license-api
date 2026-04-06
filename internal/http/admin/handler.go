package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/infrastructure/idgen"
	crypto "github.com/devravik/go-license-api/internal/security"
	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	base *handlers.Handler
}

func NewHandler(base *handlers.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Status(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"message": "Admin API - Operational",
	})
}

func (h *Handler) CreateTenant(c fiber.Ctx) error {
	var req dto.AdminCreateTenantRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	if req.RPS == 0 {
		req.RPS = 100
	}
	if req.Burst == 0 {
		req.Burst = 200
	}

	tenant, apiKey, err := h.base.AdminService.CreateTenant(c.Context(), req.RPS, req.Burst)
	if err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"tenant_id": tenant.ID,
		"api_key":   apiKey,
		"limits": fiber.Map{
			"rps":   tenant.RPS,
			"burst": tenant.Burst,
		},
	})
}

func (h *Handler) RevokeLicense(c fiber.Ctx) error {
	var req dto.AdminRevokeLicenseRequest
	_ = c.Bind().Body(&req)
	if req.TenantID == "" {
		req.TenantID = c.Params("tenant_id")
	}
	if req.Key == "" {
		req.Key = c.Params("key")
	}
	if req.TenantID == "" && req.Key == "" {
		// preserve legacy body-required validation path
		if err := c.Bind().Body(&req); err != nil {
			return errJSON(c, fiber.StatusBadRequest, "invalid_request")
		}
	}
	if req.TenantID == "" {
		req.TenantID = c.Query("tenant_id")
	}
	if req.Key == "" {
		req.Key = c.Query("key")
	}
	if req.TenantID == "" && req.Key == "" {
		if err := c.Bind().Body(&req); err != nil {
			return errJSON(c, fiber.StatusBadRequest, "invalid_request")
		}
	}
	if req.TenantID == "" && req.Key == "" {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	key := req.EffectiveLicenseKey()
	if key == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_key_required")
	}
	resolvedTenantID, err := h.resolveTenantID(c.Context(), req.TenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	// Backward-compatible: AdminService interface keeps original signature (no reason).
	if err := h.base.AdminService.RevokeLicense(c.Context(), resolvedTenantID, key); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) CreateLicense(c fiber.Ctx) error {
	var req dto.AdminCreateLicenseRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	resolvedTenantID, err := h.resolveTenantID(c.Context(), req.TenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	req.TenantID = resolvedTenantID
	if err := h.resolveOrCreateLicenseRefs(c.Context(), c.Get("Idempotency-Key"), &req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	lic, err := adminLicenseFromRequest(req)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	if err := h.base.AdminService.CreateLicense(c.Context(), lic); err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"license_id": lic.ID, "key": lic.Key})
}

func (h *Handler) GetLicense(c fiber.Ctx) error {
	tenantID := c.Params("tenant_id")
	if tenantID == "" {
		tenantID = c.Query("tenant_id")
	}
	key := c.Params("key")
	if key == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_key_required")
	}
	var err error
	tenantID, err = h.resolveTenantID(c.Context(), tenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	lic, err := h.base.AdminService.GetLicense(c.Context(), tenantID, key)
	if err != nil {
		return errJSON(c, fiber.StatusNotFound, "license_not_found")
	}
	return c.JSON(lic)
}

func (h *Handler) UpdateLicense(c fiber.Ctx) error {
	tenantID := c.Params("tenant_id")
	if tenantID == "" {
		tenantID = c.Query("tenant_id")
	}
	key := c.Params("key")
	if key == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_key_required")
	}
	var err error
	tenantID, err = h.resolveTenantID(c.Context(), tenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	var req dto.AdminUpdateLicenseRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	req.TenantID = tenantID
	req.Key = key
	lic, err := adminLicenseFromRequest(req)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	if err := h.base.AdminService.UpdateLicense(c.Context(), lic); err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) SuspendTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	var req dto.AdminSuspendTenantRequest
	_ = c.Bind().Body(&req)
	if err := h.base.AdminService.SuspendTenant(c.Context(), tenantID, req.Reason); err != nil {
		if err.Error() == "system_tenant_protected" {
			return errJSON(c, fiber.StatusForbidden, "system_tenant_protected")
		}
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) ReinstateTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	if err := h.base.AdminService.ReinstateTenant(c.Context(), tenantID); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"status": "active"})
}

func (h *Handler) DeleteTenant(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	if err := h.base.AdminService.DeleteTenant(c.Context(), tenantID); err != nil {
		if err.Error() == "system_tenant_protected" {
			return errJSON(c, fiber.StatusForbidden, "system_tenant_protected")
		}
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *Handler) RotateTenantKey(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	var req dto.AdminRotateAPIKeyRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	if req.GraceMinutes == 0 {
		req.GraceMinutes = 60
	}
	if req.GraceMinutes > 1440 {
		req.GraceMinutes = 1440
	}

	newKey, expiresAt, err := h.base.AdminService.RotateTenantAPIKey(c.Context(), tenantID, time.Duration(req.GraceMinutes)*time.Minute)
	if err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{
		"new_api_key":              newKey,
		"old_key_grace_expires_at": expiresAt,
	})
}

func (h *Handler) UpdateTenantLimits(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	var req dto.AdminUpdateTenantLimitsRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	if err := h.base.AdminService.UpdateTenantLimits(c.Context(), tenantID, req.RPS, req.Burst); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_limits")
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

func (h *Handler) UpdateTenantIPAllowlist(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	var req dto.AdminUpdateTenantIPAllowlistRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	if err := h.base.AdminService.UpdateTenantIPAllowlist(c.Context(), tenantID, req.CIDRs); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

// AdminRegisterWebhook registers a webhook for a tenant. Control plane only.
func (h *Handler) RegisterWebhook(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	var req dto.AdminRegisterWebhookRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	if req.URL == "" || len(req.Events) == 0 || req.Secret == "" {
		return errJSON(c, fiber.StatusBadRequest, "url_events_secret_required")
	}
	// SSRF guard: validate URL before persisting
	if err := crypto.IsSafeWebhookURL(req.URL); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_webhook_url")
	}
	if len(h.base.WebhookEncKey) != 32 {
		return errJSON(c, fiber.StatusInternalServerError, "webhook_encryption_key_invalid")
	}
	enc, err := crypto.EncryptAES(h.base.WebhookEncKey, []byte(req.Secret))
	if err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "encryption_failed")
	}
	if h.base.WebhookRepo == nil {
		return errJSON(c, fiber.StatusInternalServerError, "webhook_repo_unavailable")
	}
	webhookID, err := idgen.NewID("wh")
	if err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "failed_to_register")
	}
	if err := h.base.WebhookRepo.Create(c.Context(), webhookID, tenantID, req.URL, req.Events, enc); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "failed_to_register")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
}

// UpdateTenantProfile updates tenant identity and billing-profile fields if supported by the service.
func (h *Handler) UpdateTenantProfile(c fiber.Ctx) error {
	tenantID := c.Params("id")
	if tenantID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_required")
	}
	var req dto.AdminUpdateTenantProfileRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	// Optional method detection via type assertion.
	type profiler interface {
		UpdateTenantProfile(ctx fiber.Ctx, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	}
	// Use the underlying concrete value; AdminService may wrap the concrete type.
	if svc, ok := any(h.base.AdminService).(interface {
		UpdateTenantProfile(ctx context.Context, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	}); ok {
		if err := svc.UpdateTenantProfile(c.Context(), tenantID, req.Name, req.Slug, req.Email, req.Company, req.Plan, req.MaxLicenses, req.Metadata); err != nil {
			return errJSON(c, fiber.StatusInternalServerError, "internal_error")
		}
		return c.JSON(fiber.Map{"status": "updated"})
	}
	return errJSON(c, fiber.StatusNotImplemented, "update_profile_not_supported")
}

func (h *Handler) CreatePlan(c fiber.Ctx) error {
	var req dto.AdminUpsertPlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	if req.Name == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_name_required")
	}
	resolvedTenantID, err := h.resolveTenantID(c.Context(), req.TenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	req.TenantID = resolvedTenantID
	planID := req.ID
	if planID == "" {
		id, err := idgen.NewID("plan")
		if err != nil {
			return errJSON(c, fiber.StatusInternalServerError, "id_generation_failed")
		}
		planID = id
	}
	p := &domain.Plan{
		ID:          planID,
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		Name:        req.Name,
		Features:    req.Features,
		Limits:      domain.PlanLimits{Seats: req.Seats},
		IsActive:    req.IsActive,
	}
	if err := h.base.AdminService.CreatePlan(c.Context(), p); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"id": planID, "ok": true})
}

func (h *Handler) UpdatePlan(c fiber.Ctx) error {
	planID := c.Params("id")
	if planID == "" {
		return errJSON(c, fiber.StatusBadRequest, "plan_id_required")
	}
	var req dto.AdminUpsertPlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	resolvedTenantID, err := h.resolveTenantID(c.Context(), req.TenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	req.TenantID = resolvedTenantID
	p := &domain.Plan{
		ID:        planID,
		TenantID:  req.TenantID,
		ProductID: req.ProductID,
		Name:      req.Name,
		Features:  req.Features,
		Limits:    domain.PlanLimits{Seats: req.Seats},
		IsActive:  req.IsActive,
	}
	if err := h.base.AdminService.UpdatePlan(c.Context(), p); err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) GetPlan(c fiber.Ctx) error {
	tenantID := c.Query("tenant_id")
	planID := c.Params("id")
	if planID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_plan_id_required")
	}
	var err error
	tenantID, err = h.resolveTenantID(c.Context(), tenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	p, err := h.base.AdminService.GetPlan(c.Context(), tenantID, planID)
	if err != nil {
		return errJSON(c, fiber.StatusNotFound, "plan_not_found")
	}
	return c.JSON(p)
}

func (h *Handler) ListPlans(c fiber.Ctx) error {
	tenantID := c.Query("tenant_id")
	resolvedTenantID, err := h.resolveTenantID(c.Context(), tenantID)
	if err != nil {
		return errJSON(c, fiber.StatusBadRequest, err.Error())
	}
	plans, err := h.base.AdminService.ListPlans(c.Context(), resolvedTenantID)
	if err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"plans": plans})
}

func (h *Handler) resolveTenantID(ctx context.Context, tenantID string) (string, error) {
	return h.base.AdminService.ResolveTenantID(ctx, strings.TrimSpace(tenantID))
}

func (h *Handler) DeletePlan(c fiber.Ctx) error {
	tenantID := c.Params("tenant_id")
	planID := c.Params("id")
	if tenantID == "" || planID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_plan_id_required")
	}
	if err := h.base.AdminService.DeletePlan(c.Context(), tenantID, planID); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) RestorePlan(c fiber.Ctx) error {
	tenantID := c.Params("tenant_id")
	planID := c.Params("id")
	if tenantID == "" || planID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_plan_id_required")
	}
	if err := h.base.AdminService.RestorePlan(c.Context(), tenantID, planID); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) SetPlanActive(c fiber.Ctx) error {
	tenantID := c.Params("tenant_id")
	planID := c.Params("id")
	if tenantID == "" || planID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_plan_id_required")
	}
	var req dto.AdminSetPlanActiveRequest
	if err := c.Bind().Body(&req); err != nil {
		return errJSON(c, fiber.StatusBadRequest, "invalid_request")
	}
	if err := h.base.AdminService.SetPlanActive(c.Context(), tenantID, planID, req.Active); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) DeleteProduct(c fiber.Ctx) error {
	tenantID := c.Params("tenant_id")
	productID := c.Params("id")
	if tenantID == "" || productID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_product_id_required")
	}
	if err := h.base.AdminService.DeleteProduct(c.Context(), tenantID, productID); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) RestoreProduct(c fiber.Ctx) error {
	tenantID := c.Params("tenant_id")
	productID := c.Params("id")
	if tenantID == "" || productID == "" {
		return errJSON(c, fiber.StatusBadRequest, "tenant_id_and_product_id_required")
	}
	if err := h.base.AdminService.RestoreProduct(c.Context(), tenantID, productID); err != nil {
		return errJSON(c, fiber.StatusInternalServerError, "internal_error")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func adminLicenseFromRequest(req dto.AdminCreateLicenseRequest) (*domain.License, error) {
	lic := &domain.License{
		TenantID:   req.TenantID,
		Type:       req.Type,
		PlanID:     req.PlanID,
		ProductID:  req.ProductID,
		Key:        req.Key,
		Status:     req.Status,
		SeatsTotal: req.SeatsTotal,
		SeatsUsed:  req.SeatsUsed,
		Features:   req.Features,
		Overrides: domain.LicenseOverride{
			FeaturesAdd:    req.Overrides.FeaturesAdd,
			FeaturesRemove: req.Overrides.FeaturesRemove,
		},
		Metadata: req.Metadata,
	}
	if lic.Status == "" {
		lic.Status = "active"
	}
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return nil, err
		}
		lic.ExpiresAt = &t
	}
	if req.Trial.EndsAt != nil && *req.Trial.EndsAt != "" {
		t, err := time.Parse(time.RFC3339, *req.Trial.EndsAt)
		if err != nil {
			return nil, err
		}
		lic.Trial.EndsAt = &t
	}
	lic.Trial.Enabled = req.Trial.Enabled
	lic.Trial.Features = req.Trial.Features
	if lic.Trial.Enabled {
		if lic.Trial.EndsAt == nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "trial_ends_at_required")
		}
		if len(lic.Trial.Features) == 0 {
			return nil, fiber.NewError(fiber.StatusBadRequest, "trial_features_required")
		}
	}
	return lic, nil
}

func (h *Handler) resolveOrCreateLicenseRefs(ctx context.Context, idemKey string, req *dto.AdminCreateLicenseRequest) error {
	if req == nil {
		return errors.New("invalid_request")
	}
	req.Type = strings.TrimSpace(req.Type)
	if req.Type != "plan" && req.Type != "product" {
		return errors.New("invalid_type")
	}
	inlineMode := req.Plan != nil || req.Product != nil
	if inlineMode && strings.TrimSpace(idemKey) == "" {
		return errors.New("idempotency_key_required_for_inline_create")
	}

	if req.Type == "plan" {
		if (req.PlanID == nil && req.Plan == nil) || (req.PlanID != nil && req.Plan != nil) {
			return errors.New("exactly_one_of_plan_id_or_plan_required")
		}
		if req.Plan != nil {
			planID, err := h.createInlinePlan(ctx, req.TenantID, req.Plan)
			if err != nil {
				return err
			}
			req.PlanID = &planID
			req.ProductID = nil
		}
		return nil
	}

	// type == product
	if len(req.Features) == 0 {
		return errors.New("features_required_for_product_type")
	}
	if (req.ProductID == nil && req.Product == nil) || (req.ProductID != nil && req.Product != nil) {
		return errors.New("exactly_one_of_product_id_or_product_required")
	}
	if req.Product != nil {
		productID, err := h.createInlineProduct(ctx, req.TenantID, req.Product)
		if err != nil {
			return err
		}
		req.ProductID = &productID
		req.PlanID = nil
	}
	return nil
}

func (h *Handler) createInlinePlan(ctx context.Context, tenantID string, in *dto.AdminInlinePlanInput) (string, error) {
	if in == nil || strings.TrimSpace(in.Name) == "" {
		return "", errors.New("invalid_plan")
	}
	planID, err := idgen.NewID("plan")
	if err != nil {
		return "", err
	}
	var productID *string
	if in.Product != nil {
		id, err := h.createInlineProduct(ctx, tenantID, in.Product)
		if err != nil {
			return "", err
		}
		productID = &id
	}
	p := &domain.Plan{
		ID:        planID,
		TenantID:  tenantID,
		ProductID: productID,
		Name:      in.Name,
		Features:  in.Features,
		Limits:    domain.PlanLimits{Seats: in.Limits.Seats},
		IsActive:  true,
	}
	if err := h.base.AdminService.CreatePlan(ctx, p); err != nil {
		return "", err
	}
	return planID, nil
}

func (h *Handler) createInlineProduct(ctx context.Context, tenantID string, in *dto.AdminInlineProductInput) (string, error) {
	if in == nil || strings.TrimSpace(in.Name) == "" {
		return "", errors.New("invalid_product")
	}
	productID, err := idgen.NewID("prod")
	if err != nil {
		return "", err
	}
	codeID, err := idgen.NewID("pcode")
	if err != nil {
		return "", err
	}
	p := &domain.Product{
		ID:       productID,
		TenantID: tenantID,
		Code:     codeID,
		Name:     in.Name,
		Features: in.Features,
		IsActive: true,
	}
	if err := h.base.AdminService.UpsertProduct(ctx, p); err != nil {
		return "", err
	}
	return productID, nil
}

func errJSON(c fiber.Ctx, status int, code string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": dto.NewError(code, errorMessage(code)),
	})
}

func errorMessage(code string) string {
	switch code {
	case "invalid_request":
		return "Invalid request"
	case "tenant_id_required":
		return "Tenant ID is required"
	case "tenant_id_and_key_required":
		return "tenant_id and key are required"
	case "license_not_found":
		return "License not found"
	case "plan_not_found":
		return "Plan not found"
	case "invalid_limits":
		return "Invalid limits"
	case "system_tenant_protected":
		return "System tenant is protected"
	case "system_tenant_not_found":
		return "System tenant not found"
	case "internal_error":
		return "Internal server error"
	default:
		return strings.ReplaceAll(code, "_", " ")
	}
}


