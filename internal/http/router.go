package http

import (
	"log"
	"time"

	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/audit"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/http/handlers"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	"github.com/devravik/go-license-api/internal/infrastructure/crypto"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
)

// SetupRoutes is the legacy signature used by existing tests.
// It configures core routes only. Signed license and JWKS are not registered here.
func SetupRoutes(
	app *fiber.App,
	cfg *configs.Config,
	valSvc app.ValidationService,
	activationSvc app.ActivationService,
	adminSvc app.AdminService,
	pool *worker.Pool,
	tenantStore *cache.TenantStore,
	rateLimiter *middleware.RateLimiter,
	// NOTE: do not add new parameters to this legacy function
) {
	SetupRoutesV2(app, cfg, valSvc, activationSvc, adminSvc, pool, tenantStore, rateLimiter, nil, nil, nil, nil, nil)
}

// SetupRoutesV2 is the extended router that wires signed licenses and JWKS when dependencies are provided.
func SetupRoutesV2(
	app *fiber.App,
	cfg *configs.Config,
	valSvc app.ValidationService,
	activationSvc app.ActivationService,
	adminSvc app.AdminService,
	pool *worker.Pool,
	tenantStore *cache.TenantStore,
	rateLimiter *middleware.RateLimiter,
	licenseStore *cache.LicenseStore,
	signerRegistry *crypto.SignerRegistry,
	auditQuery *audit.QueryService,
	webhookEncKey []byte,
	webhookRepo handlers.WebhookWriter,
) {
	idempCache, err := handlers.NewIdempotencyCache(10_000)
	if err != nil {
		log.Printf("idempotency cache disabled: %v", err)
	}
	// webhookEncKey is populated in server.New and passed via cfg.WebhookEncKeyHex decoded
	// Router cannot decode; server provides the bytes and webhook repo during construction.
	h := handlers.NewHandler(cfg, valSvc, activationSvc, adminSvc, pool, idempCache, licenseStore, signerRegistry, auditQuery, webhookEncKey, webhookRepo)

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
	licenseGroup.Use(middleware.TenantAuth(cfg.AppMode, nil, tenantStore))
	licenseGroup.Use(rateLimiter.Middleware())
	licenseGroup.Post("/validate", h.Validate)
	licenseGroup.Post("/activate", h.Activate)
	licenseGroup.Post("/deactivate", h.Deactivate)

	// Signed license issuance (Tenant Protected, BYPASSES rate limiter and worker pool) if deps are present.
	if licenseStore != nil && signerRegistry != nil {
		app.Get("/licenses/:key/signed", middleware.TenantAuth(cfg.AppMode, nil, tenantStore), h.GetSignedLicense)
	}

	// Admin Control Plane (Protected)
	adminGroup := app.Group("/admin")
	adminGroup.Use(middleware.AdminCIDRGuard(cfg.AdminAllowedCIDRs))
	adminGroup.Use(middleware.AdminKeyGuard(cfg.AdminKey))
	adminGroup.Get("/", h.AdminStatus)
	adminGroup.Post("/tenants", h.AdminCreateTenant)
	adminGroup.Post("/licenses/revoke", h.AdminRevokeLicense)
	adminGroup.Post("/tenants/:id/suspend", h.AdminSuspendTenant)
	adminGroup.Post("/tenants/:id/reinstate", h.AdminReinstateTenant)
	adminGroup.Post("/tenants/:id/ip-allowlist", h.AdminUpdateTenantIPAllowlist)
	adminGroup.Post("/tenants/:id/webhooks", h.AdminRegisterWebhook)
	adminGroup.Post("/tenants/:id/rotate-key", h.AdminRotateTenantKey)
	adminGroup.Patch("/tenants/:id/limits", h.AdminUpdateTenantLimits)
	adminGroup.Delete("/tenants/:id", h.AdminDeleteTenant)
	// Backward-compatible alias for older clients.
	adminGroup.Post("/tenants/:id/rotate_key", h.AdminRotateTenantKey)
	// Audit log query (Admin Control Plane)
	adminGroup.Get("/audit-log", h.AdminQueryAuditLog)

	// JWKS (Public) if deps are present.
	if signerRegistry != nil {
		app.Get("/.well-known/jwks.json", h.JWKS)
	}
}
