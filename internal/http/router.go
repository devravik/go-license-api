package http

import (
	"time"

	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
)

func SetupRoutes(app *fiber.App, cfg *configs.Config, valSvc app.ValidationService, adminSvc app.AdminService, pool *worker.Pool) {
	h := handlers.NewHandler(cfg, valSvc, adminSvc, pool)

	// Landing Page
	app.Get("/", timeout.New(h.Home, timeout.Config{
		Timeout: 5 * time.Second,
	}))

	// Health check (Public)
	app.Get("/health", timeout.New(h.Health, timeout.Config{
		Timeout: 2 * time.Second,
	}))

	// License Validation (Tenant Protected)
	licenseGroup := app.Group("/licenses")
	licenseGroup.Use(middleware.Tenant)
	licenseGroup.Use(middleware.RateLimit)
	licenseGroup.Post("/validate", h.Validate)

	// Admin Control Plane (Protected)
	adminGroup := app.Group("/admin")
	adminGroup.Use(middleware.Auth)
	adminGroup.Get("/", h.AdminStatus)
	adminGroup.Post("/licenses/revoke", h.AdminRevokeLicense)
	adminGroup.Post("/tenants/:id/suspend", h.AdminSuspendTenant)
	adminGroup.Post("/tenants/:id/rotate_key", h.AdminRotateTenantKey)
}
