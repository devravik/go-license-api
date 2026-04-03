package license

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	base *handlers.Handler
}

var (
	validationRespValidTrue      = []byte(`{"valid":true}`)
	validationRespInvalidReqBody = []byte(`{"valid":false,"error":"invalid_request_body"}`)
	validationRespKeyRequired    = []byte(`{"valid":false,"error":"key_is_required"}`)
	validationRespTimeout        = []byte(`{"valid":false,"error":"validation_timeout"}`)
	validationRespInternalErr    = []byte(`{"valid":false,"error":"internal_validation_error"}`)
)

func NewHandler(base *handlers.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Validate(c fiber.Ctx) error {
	var req dto.LicenseValidationRequest
	if err := c.Bind().Body(&req); err != nil {
		c.Type("json")
		return c.Status(fiber.StatusBadRequest).Send(validationRespInvalidReqBody)
	}

	if req.Key == "" {
		c.Type("json")
		return c.Status(fiber.StatusBadRequest).Send(validationRespKeyRequired)
	}

	apiKey, _ := c.Locals("api_key").(string)
	tenantID, _ := c.Locals("tenant_id").(string)

	// Call validation service directly — validation is pure in-memory (L1/L2 cache
	// lookup + business rules), so routing through the worker pool only adds channel
	// hops and goroutine scheduling overhead. Fiber/fasthttp already manages
	// concurrency; the worker pool remains available for future I/O-bound tasks.
	ctx, cancel := context.WithTimeout(c.Context(), h.base.Cfg.ValidationTimeout)
	defer cancel()

	result, err := h.base.ValidationService.Validate(ctx, tenantID, apiKey, req.Key, req.Product)
	if err != nil {
		if ctx.Err() != nil {
			c.Type("json")
			return c.Status(fiber.StatusGatewayTimeout).Send(validationRespTimeout)
		}
		c.Type("json")
		return c.Status(fiber.StatusInternalServerError).Send(validationRespInternalErr)
	}
	// Fast-path: most successful validations have no extra metadata.
	if result.Valid && result.Meta == nil && result.Error == "" {
		c.Type("json")
		return c.Status(fiber.StatusOK).Send(validationRespValidTrue)
	}
	return c.Status(fiber.StatusOK).JSON(dto.LicenseValidationResponse{
		Valid: result.Valid,
		Meta:  result.Meta,
		Error: result.Error,
	})
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
			Recorded: false,
			Error:    "usage_not_enabled",
		})
	}
	tenantID, _ := c.Locals("tenant_id").(string)
	var req dto.UsageRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Recorded: false,
			Error:    "invalid_request",
		})
	}
	// Pre-cache, pre-any IO: strict request validation.
	if req.Key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Recorded: false,
			Error:    "key_required",
		})
	}
	if len(req.Key) < h.base.Cfg.MinLicenseKeyLen {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Recorded: false,
			Error:    "invalid_key",
		})
	}
	if req.Units <= 0 || req.Units > 1_000_000 {
		return c.Status(fiber.StatusBadRequest).JSON(dto.UsageResponse{
			Recorded: false,
			Error:    "invalid_units",
		})
	}
	if err := h.base.ActivationService.RecordUsage(c.Context(), tenantID, req.Key, req.Units); err != nil {
		switch err {
		case domain.ErrLicenseNotFound:
			return c.Status(fiber.StatusNotFound).JSON(dto.UsageResponse{Recorded: false, Error: "license_not_found"})
		case domain.ErrLicenseRevoked:
			return c.Status(fiber.StatusForbidden).JSON(dto.UsageResponse{Recorded: false, Error: "license_revoked"})
		case domain.ErrLicenseExpired:
			return c.Status(fiber.StatusPaymentRequired).JSON(dto.UsageResponse{Recorded: false, Error: "license_expired"})
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(dto.UsageResponse{
				Recorded: false,
				Error:    "internal_error",
			})
		}
	}
	return c.JSON(dto.UsageResponse{
		Recorded: true,
	})
}
