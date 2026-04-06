package license

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/http/handlers"
	security "github.com/devravik/go-license-api/internal/security"
	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	base *handlers.Handler
}

var (
	validationRespValidTrue = []byte(`{"success":true,"valid":true}`)
)

func NewHandler(base *handlers.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Validate(c fiber.Ctx) error {
	var req dto.LicenseValidationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.LicenseValidationResponse{
			Success: false,
			Valid:   false,
			Error:   dto.NewError("invalid_request_body", "Invalid request body"),
		})
	}

	licenseKey := req.EffectiveLicenseKey()
	if licenseKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.LicenseValidationResponse{
			Success: false,
			Valid:   false,
			Error:   dto.NewError("key_is_required", "license_key is required"),
		})
	}

	// Public data plane: cache-only lookup by global license key.
	ctx, cancel := context.WithTimeout(c.Context(), h.base.Cfg.ValidationTimeout)
	defer cancel()
	if h.base.LicenseStore == nil {
		tenantID := strings.TrimSpace(c.Get("X-Tenant-ID"))
		apiKey := strings.TrimSpace(c.Get("X-API-Key"))
		if tenantID == "" || apiKey == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(dto.LicenseValidationResponse{
				Success: false,
				Valid:   false,
				Error:   dto.NewError("unauthorized", "Tenant credentials required"),
			})
		}
		result, err := h.base.ValidationService.Validate(ctx, tenantID, security.HashAPIKey(apiKey), licenseKey, req.EffectiveProductID())
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return c.Status(fiber.StatusGatewayTimeout).JSON(dto.LicenseValidationResponse{Success: false, Valid: false, Error: dto.NewError("timeout", "Validation timeout")})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(dto.LicenseValidationResponse{Success: false, Valid: false, Error: dto.NewError("internal_error", "Internal error")})
		}
		if !result.Valid {
			return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{Success: false, Valid: false, Error: validationError(result.Error)})
		}
		return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
			Success: true,
			Valid:   true,
			License: result.Meta,
		})
	}
	lic, err := h.base.LicenseStore.GetByGlobalKey(ctx, licenseKey)
	if err != nil || lic == nil {
		return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
			Success:   false,
			Valid:     false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("license_not_found", "License not found"),
		})
	}
	productID := req.EffectiveProductID()
	if productID != "" {
		if lic.ProductID != nil && *lic.ProductID != "" && *lic.ProductID != productID {
			return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
				Success:   false,
				Valid:     false,
				RequestID: requestID(c),
				Timestamp: nowISO(),
				Error:     dto.NewError("invalid_product", "Invalid product"),
			})
		}
		if lic.Product != "" && lic.Product != productID {
			return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
				Success:   false,
				Valid:     false,
				RequestID: requestID(c),
				Timestamp: nowISO(),
				Error:     dto.NewError("invalid_product", "Invalid product"),
			})
		}
	}
	if lic.IsRevoked() {
		return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
			Success:   false,
			Valid:     false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("license_revoked", "License revoked"),
		})
	}
	inGrace := lic.IsInGracePeriod()
	if lic.IsExpired() && !inGrace {
		return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
			Success:   false,
			Valid:     false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("license_expired", "License expired"),
		})
	}
	// Success
	var planRef *domain.ValidationRef
	planID := ""
	if lic.PlanID != nil && *lic.PlanID != "" {
		planID = *lic.PlanID
		planRef = &domain.ValidationRef{ID: *lic.PlanID}
	} else if lic.Plan != "" {
		planID = lic.Plan
		planRef = &domain.ValidationRef{ID: lic.Plan, Name: lic.Plan}
	}
	var productRef *domain.ValidationRef
	productRespID := ""
	if lic.ProductID != nil {
		productRespID = *lic.ProductID
		productRef = &domain.ValidationRef{ID: *lic.ProductID}
	} else if lic.Product != "" {
		productRespID = lic.Product
		productRef = &domain.ValidationRef{ID: lic.Product, Name: lic.Product}
	}
	seatsTotal := lic.SeatsTotal
	meta := &domain.ValidationMeta{
		LicenseID:         lic.ID,
		Status:            lic.Status,
		Type:              lic.Type,
		PlanID:            planID,
		Plan:              planRef,
		Product:           productRef,
		ProductID:         productRespID,
		ExpiresAt:         lic.ExpiresAt,
		SeatsTotal:        &seatsTotal,
		UnlimitedSeats:    seatsTotal == -1,
		Trial:             lic.IsTrial,
		GracePeriodEndsAt: lic.GracePeriodEndsAt(),
		Features:          lic.FinalFeatures,
		Version:           lic.Version,
		InGracePeriod:     inGrace,
	}
	if len(meta.Features) == 0 {
		meta.Features = lic.Features
	}
	// Fast path for no meta
	if meta.Plan == nil && meta.Product == nil && meta.ExpiresAt == nil && len(meta.Features) == 0 && meta.SeatsTotal == nil && !meta.Trial && meta.GracePeriodEndsAt == nil {
		c.Type("json")
		return c.Status(fiber.StatusOK).Send(validationRespValidTrue)
	}
	return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
		Success:   true,
		Valid:     true,
		License:   meta,
		RequestID: requestID(c),
		Timestamp: nowISO(),
	})
}

func validationError(code string) *dto.APIError {
	switch code {
	case "":
		return nil
	case "license_not_found":
		return dto.NewError(code, "License not found")
	case "license_revoked":
		return dto.NewError(code, "License revoked")
	case "license_expired":
		return dto.NewError(code, "License expired")
	case "invalid_product":
		return dto.NewError(code, "Invalid product")
	default:
		return dto.NewError(code, "Validation failed")
	}
}

func normalizeClientID(id string) string {
	s := strings.TrimSpace(strings.ToLower(id))
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			s = u.Host
		}
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		hostPart := s[:i]
		if !strings.Contains(hostPart, "]") && !strings.Contains(hostPart, ":") {
			s = hostPart
		}
	}
	if strings.HasPrefix(s, "www.") {
		s = strings.TrimPrefix(s, "www.")
	}
	return strings.TrimSuffix(s, ".")
}

func (h *Handler) Activate(c fiber.Ctx) error {
	return h.base.Activate(c)
}

func (h *Handler) Deactivate(c fiber.Ctx) error {
	return h.base.Deactivate(c)
}

// Usage records consumption units for a license key.
func (h *Handler) Usage(c fiber.Ctx) error {
	if h.base.ActivationService == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(dto.UsageResponse{
			Success:   false,
			Recorded:  false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("usage_not_enabled", "Usage recording is not enabled"),
		})
	}
	var req dto.UsageRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Success:   false,
			Recorded:  false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("invalid_request", "Invalid request body"),
		})
	}
	// Pre-cache, pre-any IO: strict request validation.
	licenseKey := req.EffectiveLicenseKey()
	if licenseKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Success:   false,
			Recorded:  false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("key_required", "license_key is required"),
		})
	}
	if len(licenseKey) < h.base.Cfg.MinLicenseKeyLen {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Success:   false,
			Recorded:  false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("invalid_key", "Invalid license key"),
		})
	}
	if h.base.LicenseStore == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(dto.UsageResponse{
			Success:   false,
			Recorded:  false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("cache_unavailable", "License cache unavailable"),
		})
	}
	lic, _ := h.base.LicenseStore.GetByGlobalKey(c.Context(), licenseKey)
	if lic == nil {
		return c.Status(fiber.StatusNotFound).JSON(dto.UsageResponse{
			Success:   false,
			Recorded:  false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("license_not_found", "License not found"),
		})
	}
	tenantID := lic.TenantID
	if req.Units <= 0 || req.Units > 1_000_000 {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Success:   false,
			Recorded:  false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("invalid_units", "Invalid usage units"),
		})
	}
	total, remaining, err := h.base.ActivationService.RecordUsage(c.Context(), tenantID, licenseKey, req.Units)
	if err != nil {
		switch err {
		case domain.ErrLicenseNotFound:
			return c.Status(fiber.StatusNotFound).JSON(dto.UsageResponse{Success: false, Recorded: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("license_not_found", "License not found")})
		case domain.ErrLicenseRevoked:
			return c.Status(fiber.StatusForbidden).JSON(dto.UsageResponse{Success: false, Recorded: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("license_revoked", "License revoked")})
		case domain.ErrLicenseExpired:
			return c.Status(fiber.StatusPaymentRequired).JSON(dto.UsageResponse{Success: false, Recorded: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("license_expired", "License expired")})
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(dto.UsageResponse{
				Success:   false,
				Recorded:  false,
				RequestID: requestID(c),
				Timestamp: nowISO(),
				Error:     dto.NewError("internal_error", "Internal server error"),
			})
		}
	}
	return c.JSON(dto.UsageResponse{
		Success:   true,
		Recorded:  true,
		RequestID: requestID(c),
		Timestamp: nowISO(),
		Usage: &struct {
			TotalUsed int  `json:"total_used"`
			Remaining *int `json:"remaining,omitempty"`
		}{TotalUsed: total, Remaining: remaining},
	})
}

func requestID(c fiber.Ctx) string {
	if id := strings.TrimSpace(c.Get("X-Request-ID")); id != "" {
		return id
	}
	return fmt.Sprintf("req-%d", time.Now().UTC().UnixNano())
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
