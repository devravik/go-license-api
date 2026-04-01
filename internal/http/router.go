package http

import (
	"time"

	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
)

func SetupRoutes(app *fiber.App, cfg *configs.Config, valSvc app.ValidationService) {
	h := handlers.NewHandler(cfg, valSvc)

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
	adminGroup.Get("/dashboard", h.AdminDashboard)
}
