package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/dto"
	"github.com/gofiber/fiber/v3"
)

func normalizeClientID(id string) string {
	// Normalize common domain/url forms to a stable host token.
	// - lowercase
	// - strip scheme, path, query, fragment
	// - drop leading "www."
	// - trim trailing dot
	s := strings.TrimSpace(strings.ToLower(id))
	if s == "" {
		return s
	}
	// Fast-path: if looks like URL, parse host.
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			s = u.Host
		}
	}
	// If still contains a slash, take up to first slash.
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	// Strip userinfo if accidentally provided.
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	// Strip port if present.
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		hostPart := s[:i]
		// avoid stripping on IPv6
		if !strings.Contains(hostPart, "]") && !strings.Contains(hostPart, ":") {
			s = hostPart
		}
	}
	// Drop leading www.
	if strings.HasPrefix(s, "www.") {
		s = strings.TrimPrefix(s, "www.")
	}
	s = strings.TrimSuffix(s, ".")
	return s
}
func (h *Handler) Activate(c fiber.Ctx) error {
	if h.ActivationService == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(dto.ActivateResponse{
			Success:   false,
			Activated: false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("activation_not_enabled", "Activation is not enabled"),
		})
	}

	tenantID, _ := c.Locals("tenant_id").(string)
	idempotencyKey := c.Get("Idempotency-Key")
	if idempotencyKey != "" && h.IdempCache != nil {
		if cached, ok := h.IdempCache.Get(tenantID, idempotencyKey); ok {
			return c.JSON(cached)
		}
	}

	var req dto.ActivateRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.ActivateResponse{
			Success:   false,
			Activated: false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("invalid_request", "Invalid request body"),
		})
	}
	licenseKey := req.EffectiveLicenseKey()
	clientID := normalizeClientID(req.EffectiveClientID())
	if licenseKey == "" || clientID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.ActivateResponse{
			Success:   false,
			Activated: false,
			RequestID: requestID(c),
			Timestamp: nowISO(),
			Error:     dto.NewError("key_and_client_id_required", "license_key and client_id are required"),
		})
	}

	record, remaining, totalSeats, err := h.ActivationService.Activate(c.Context(), tenantID, licenseKey, clientID, req.Hostname)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrSeatLimitReached):
			return c.Status(fiber.StatusForbidden).JSON(dto.ActivateResponse{
				Success:   false,
				Activated: false,
				RequestID: requestID(c),
				Timestamp: nowISO(),
				Error:     dto.NewError("seats_limit_exceeded", "Seat limit reached"),
			})
		case errors.Is(err, domain.ErrLicenseNotFound):
			return c.Status(fiber.StatusNotFound).JSON(dto.ActivateResponse{Success: false, Activated: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("license_not_found", "License not found")})
		case errors.Is(err, domain.ErrLicenseExpired):
			return c.Status(fiber.StatusPaymentRequired).JSON(dto.ActivateResponse{Success: false, Activated: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("license_expired", "License expired")})
		case errors.Is(err, domain.ErrLicenseRevoked):
			return c.Status(fiber.StatusForbidden).JSON(dto.ActivateResponse{Success: false, Activated: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("license_revoked", "License revoked")})
		case errors.Is(err, domain.ErrLicenseGracePeriod):
			return c.Status(fiber.StatusForbidden).JSON(dto.ActivateResponse{Success: false, Activated: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("license_in_grace_period", "License is in grace period")})
		default:
			log.Printf("event=activate_error tenant=%s key=%s client_id=%s err=%v", tenantID, licenseKey, clientID, err)
			return c.Status(fiber.StatusInternalServerError).JSON(dto.ActivateResponse{Success: false, Activated: false, RequestID: requestID(c), Timestamp: nowISO(), Error: dto.NewError("internal_error", "Internal server error")})
		}
	}

	resp := dto.ActivateResponse{
		Success:      true,
		Activated:    true,
		ActivationID: record.ID,
		ClientID:     record.ClientID,
		RequestID:    requestID(c),
		Timestamp:    nowISO(),
	}
	if totalSeats == -1 {
		resp.UnlimitedSeats = true
		resp.SeatsRemaining = nil
	} else if remaining >= 0 && totalSeats >= 0 {
		resp.SeatsRemaining = &remaining
		resp.Seats = &struct {
			Used      int  `json:"used"`
			Total     int  `json:"total"`
			Remaining *int `json:"remaining"`
		}{
			Used:      totalSeats - remaining,
			Total:     totalSeats,
			Remaining: &remaining,
		}
	}
	if h.LicenseStore != nil {
		if lic, err := h.LicenseStore.GetByGlobalKey(c.Context(), licenseKey); err == nil && lic != nil {
			resp.License = activationLicenseMeta(lic, remaining, totalSeats)
		}
	}

	if idempotencyKey != "" && h.IdempCache != nil {
		h.IdempCache.Set(tenantID, idempotencyKey, resp, 24*time.Hour)
	}
	return c.JSON(resp)
}

func (h *Handler) Deactivate(c fiber.Ctx) error {
	if h.ActivationService == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(dto.DeactivateResponse{
			Success:     false,
			Deactivated: false,
			RequestID:   requestID(c),
			Timestamp:   nowISO(),
			Error:       dto.NewError("activation_not_enabled", "Activation is not enabled"),
		})
	}

	tenantID, _ := c.Locals("tenant_id").(string)

	var req dto.DeactivateRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.DeactivateResponse{
			Success:     false,
			Deactivated: false,
			RequestID:   requestID(c),
			Timestamp:   nowISO(),
			Error:       dto.NewError("invalid_request", "Invalid request body"),
		})
	}
	licenseKey := req.EffectiveLicenseKey()
	clientID := normalizeClientID(req.EffectiveClientID())
	if licenseKey == "" || clientID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(dto.DeactivateResponse{
			Success:     false,
			Deactivated: false,
			RequestID:   requestID(c),
			Timestamp:   nowISO(),
			Error:       dto.NewError("key_and_client_id_required", "license_key and client_id are required"),
		})
	}

	if err := h.ActivationService.Deactivate(c.Context(), tenantID, licenseKey, clientID); err != nil {
		if errors.Is(err, domain.ErrLicenseNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(dto.DeactivateResponse{
				Success:     false,
				Deactivated: false,
				RequestID:   requestID(c),
				Timestamp:   nowISO(),
				Error:       dto.NewError("license_not_found", "License not found"),
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(dto.DeactivateResponse{
			Success:     false,
			Deactivated: false,
			RequestID:   requestID(c),
			Timestamp:   nowISO(),
			Error:       dto.NewError("internal_error", "Internal server error"),
		})
	}
	return c.JSON(dto.DeactivateResponse{Success: true, Deactivated: true, RequestID: requestID(c), Timestamp: nowISO()})
}

func requestID(c fiber.Ctx) string {
	id := strings.TrimSpace(c.Get("X-Request-ID"))
	if id != "" {
		return id
	}
	return fmt.Sprintf("req-%d", time.Now().UTC().UnixNano())
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func activationLicenseMeta(lic *domain.License, remaining, totalSeats int) *domain.ValidationMeta {
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
	productID := ""
	if lic.ProductID != nil && *lic.ProductID != "" {
		productID = *lic.ProductID
		productRef = &domain.ValidationRef{ID: *lic.ProductID}
	} else if lic.Product != "" {
		productID = lic.Product
		productRef = &domain.ValidationRef{ID: lic.Product, Name: lic.Product}
	}
	features := lic.FinalFeatures
	if len(features) == 0 {
		features = lic.Features
	}
	seatsTotal := totalSeats
	if seatsTotal == 0 && lic.SeatsTotal != 0 {
		seatsTotal = lic.SeatsTotal
	}
	meta := &domain.ValidationMeta{
		LicenseID:         lic.ID,
		Status:            lic.Status,
		Type:              lic.Type,
		PlanID:            planID,
		Plan:              planRef,
		Product:           productRef,
		ProductID:         productID,
		ExpiresAt:         lic.ExpiresAt,
		SeatsTotal:        &seatsTotal,
		UnlimitedSeats:    seatsTotal == -1,
		Trial:             lic.Trial.Enabled || lic.IsTrial,
		GracePeriodEndsAt: lic.GracePeriodEndsAt(),
		Features:          features,
		Version:           lic.Version,
		InGracePeriod:     lic.IsInGracePeriod(),
	}
	if seatsTotal == -1 {
		meta.SeatsTotal = &seatsTotal
	} else if remaining >= 0 && seatsTotal >= 0 {
		meta.SeatsTotal = &seatsTotal
	}
	return meta
}
