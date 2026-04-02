package middleware

import (
	"crypto/subtle"
	"net"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func AdminKeyGuard(adminKey string) fiber.Handler {
	if strings.TrimSpace(adminKey) == "" {
		panic("ADMIN_API_KEY must be set")
	}

	return func(c fiber.Ctx) error {
		header := strings.TrimSpace(c.Get("X-Admin-Key"))
		if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(adminKey)) != 1 {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Valid: false,
				Error: "invalid_admin_key",
			})
		}
		return c.Next()
	}
}

func AdminCIDRGuard(allowedCIDRs []string) fiber.Handler {
	networks := parseCIDRs(allowedCIDRs)
	return func(c fiber.Ctx) error {
		if len(networks) == 0 {
			return c.Next()
		}
		ip := net.ParseIP(c.IP())
		for _, network := range networks {
			if network.Contains(ip) {
				return c.Next()
			}
		}
		return c.Status(fiber.StatusForbidden).JSON(ErrorResponse{
			Valid: false,
			Error: "ip_not_allowed",
		})
	}
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err == nil {
			nets = append(nets, network)
		}
	}
	return nets
}
