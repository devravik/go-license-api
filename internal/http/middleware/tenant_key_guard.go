package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"

	security "github.com/devravik/go-license-api/internal/security"
	"github.com/gofiber/fiber/v3"
)

const tenantIDLocalKey = "tenant_id"
const apiKeyFpLocalKey = "api_key_fingerprint"

type TenantKeyRecord struct {
	TenantID string
	Status   string   // "active", "rotated", "revoked"
	CIDRs    []string // CIDR strings
}

type TenantKeyLookup interface {
	GetByAPIKeyHash(ctx context.Context, apiKeyHash string) (*TenantKeyRecord, error)
}

func TenantKeyGuard(lookup TenantKeyLookup) fiber.Handler {
	return func(c fiber.Ctx) error {
		rawKey := strings.TrimSpace(c.Get("X-API-Key"))
		if rawKey == "" {
			c.Set("WWW-Authenticate", `ApiKey realm="tenant"`)
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("missing_api_key", "Missing API key"))
		}
		if len(rawKey) < 16 {
			c.Set("WWW-Authenticate", `ApiKey realm="tenant"`)
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("invalid_api_key_format", "Invalid API key format"))
		}
		apiKeyHash := security.HashAPIKey(rawKey)
		rec, err := lookup.GetByAPIKeyHash(c.Context(), apiKeyHash)
		if err != nil || rec == nil {
			c.Set("WWW-Authenticate", `ApiKey realm="tenant"`)
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("invalid_api_key", "Invalid API key"))
		}
		if strings.ToLower(rec.Status) != "active" {
			c.Set("WWW-Authenticate", `ApiKey realm="tenant"`)
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("invalid_api_key", "API key not active"))
		}
		remoteIP := clientIP(c)
		if !ipAllowed(remoteIP, rec.CIDRs) {
			return c.Status(fiber.StatusForbidden).JSON(errorResponse("ip_not_allowed", "IP not allowed"))
		}
		c.Locals(tenantIDLocalKey, rec.TenantID)
		c.Locals(apiKeyFpLocalKey, fingerprint(apiKeyHash))
		return c.Next()
	}
}

func clientIP(c fiber.Ctx) string {
	ip := strings.TrimSpace(c.IP())
	if ip == "" {
		return "0.0.0.0"
	}
	return ip
}

func ipAllowed(ip string, cidrs []string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	if len(cidrs) == 0 {
		return true
	}
	for _, c := range cidrs {
		_, nw, err := net.ParseCIDR(strings.TrimSpace(c))
		if err != nil {
			continue
		}
		if nw.Contains(parsedIP) {
			return true
		}
	}
	return false
}

func fingerprint(apiKeyHash string) string {
	// If apiKeyHash is hex already, keep prefix; else calculate from bytes
	if len(apiKeyHash) >= 12 {
		return apiKeyHash[:12]
	}
	sum := sha256.Sum256([]byte(apiKeyHash))
	return hex.EncodeToString(sum[:])[:12]
}
