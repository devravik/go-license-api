package license

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/http/handlers"
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
	// Optional: client identifier for domain/device-aware validation policies (future use).
	_ = normalizeClientID(req.EffectiveClientID())
	if licenseKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.LicenseValidationResponse{
			Success: false,
			Valid:   false,
			Error:   dto.NewError("key_is_required", "license_key is required"),
		})
	}

	apiKey, _ := c.Locals("api_key").(string)
	tenantID, _ := c.Locals("tenant_id").(string)

	// Call validation service directly — validation is pure in-memory (L1/L2 cache
	// lookup + business rules), so routing through the worker pool only adds channel
	// hops and goroutine scheduling overhead. Fiber/fasthttp already manages
	// concurrency; the worker pool remains available for future I/O-bound tasks.
	ctx, cancel := context.WithTimeout(c.Context(), h.base.Cfg.ValidationTimeout)
	defer cancel()

	result, err := h.base.ValidationService.Validate(ctx, tenantID, apiKey, licenseKey, req.Product)
	if err != nil {
		if ctx.Err() != nil {
			return c.Status(fiber.StatusGatewayTimeout).JSON(dto.LicenseValidationResponse{
				Success: false,
				Valid:   false,
				Error:   dto.NewError("validation_timeout", "Validation timeout"),
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(dto.LicenseValidationResponse{
			Success: false,
			Valid:   false,
			Error:   dto.NewError("internal_validation_error", "Internal validation error"),
		})
	}
	// Fast-path: most successful validations have no extra metadata.
	if result.Valid && result.Meta == nil && result.Error == "" {
		c.Type("json")
		return c.Status(fiber.StatusOK).Send(validationRespValidTrue)
	}
	return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
		Success:   result.Valid,
		Valid:     result.Valid,
		License:   result.Meta,
		RequestID: requestID(c),
		Timestamp: nowISO(),
		Error:     validationError(result.Error),
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
	tenantID, _ := c.Locals("tenant_id").(string)
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
