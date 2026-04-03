package http

import (
	"log"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/audit"
	adminhttp "github.com/devravik/go-license-api/internal/http/admin"
	audithttp "github.com/devravik/go-license-api/internal/http/audit"
	"github.com/devravik/go-license-api/internal/http/handlers"
	licensehttp "github.com/devravik/go-license-api/internal/http/license"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	crypto "github.com/devravik/go-license-api/internal/security"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
)

// SetupRoutes is the legacy signature used by existing tests.
// It configures core routes only. Signed license and JWKS are not registered here.
func SetupRoutes(
	app *fiber.App,
	cfg *setup.Config,
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
// Backward-compatible signature used by tests and older call sites.
func SetupRoutesV2(
	app *fiber.App,
	cfg *setup.Config,
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
	// Defaults: ready and use pool for queue depth if available.
	h.IsReady = func() bool { return true }
	h.QueueDepth = func() int {
		if pool != nil {
			return pool.QueueDepth()
		}
		return 0
	}
	licenseHandler := licensehttp.NewHandler(h)
	adminHandler := adminhttp.NewHandler(h)
	auditHandler := audithttp.NewHandler(h)

	// Landing Page
	app.Get("/", timeout.New(h.Home, timeout.Config{
		Timeout: 5 * time.Second,
	}))

	// Health check (Public)
	app.Get("/health", timeout.New(h.Health, timeout.Config{
		Timeout: 2 * time.Second,
	}))
	// Kubernetes-standard health endpoints
	app.Get("/healthz", timeout.New(h.Health, timeout.Config{
		Timeout: 2 * time.Second,
	}))
	app.Get("/readyz", timeout.New(h.Ready, timeout.Config{
		Timeout: 2 * time.Second,
	}))

	// Spec and Docs (Public, static)
	app.Get("/openapi.yaml", h.OpenAPIRawYAML)
	app.Get("/openapi.json", h.OpenAPIRawJSON)
	app.Get("/docs", h.SwaggerUI)
	app.Get("/redoc", h.RedocUI)

	// License Validation (Tenant Protected)
	licenseGroup := app.Group("/licenses")
	licenseGroup.Use(middleware.TenantAuth(cfg.AppMode, nil, tenantStore))
	licenseGroup.Use(rateLimiter.Middleware())
	licenseGroup.Post("/validate", licenseHandler.Validate)
	licenseGroup.Post("/activate", licenseHandler.Activate)
	licenseGroup.Post("/deactivate", licenseHandler.Deactivate)
	licenseGroup.Post("/usage", licenseHandler.Usage)

	// Signed license issuance (Tenant Protected, BYPASSES rate limiter and worker pool) if deps are present.
	if licenseStore != nil && signerRegistry != nil {
		app.Get("/licenses/:key/signed", middleware.TenantAuth(cfg.AppMode, nil, tenantStore), h.GetSignedLicense)
	}

	// Admin Control Plane (Protected)
	adminGroup := app.Group("/admin")
	adminGroup.Use(middleware.AdminCIDRGuard(cfg.AdminAllowedCIDRs))
	adminGroup.Use(middleware.AdminKeyGuard(cfg.AdminKey))
	adminGroup.Get("/", adminHandler.Status)
	adminGroup.Post("/tenants", adminHandler.CreateTenant)
	adminGroup.Post("/licenses/revoke", adminHandler.RevokeLicense)
	adminGroup.Post("/tenants/:id/suspend", adminHandler.SuspendTenant)
	adminGroup.Post("/tenants/:id/reinstate", adminHandler.ReinstateTenant)
	adminGroup.Post("/tenants/:id/ip-allowlist", adminHandler.UpdateTenantIPAllowlist)
	adminGroup.Patch("/tenants/:id/profile", adminHandler.UpdateTenantProfile)
	adminGroup.Post("/tenants/:id/webhooks", adminHandler.RegisterWebhook)
	adminGroup.Post("/tenants/:id/rotate-key", adminHandler.RotateTenantKey)
	adminGroup.Patch("/tenants/:id/limits", adminHandler.UpdateTenantLimits)
	adminGroup.Delete("/tenants/:id", adminHandler.DeleteTenant)
	// Backward-compatible alias for older clients.
	adminGroup.Post("/tenants/:id/rotate_key", adminHandler.RotateTenantKey)
	// Audit log query (Admin Control Plane)
	adminGroup.Get("/audit-log", auditHandler.Query)

	// JWKS (Public) if deps are present.
	if signerRegistry != nil {
		app.Get("/.well-known/jwks.json", h.JWKS)
	}
}

// SetupRoutesV3 allows injecting readiness and queue depth providers.
func SetupRoutesV3(
	app *fiber.App,
	cfg *setup.Config,
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
	isReady func() bool,
	queueDepth func() int,
) {
	// Call V2 to construct handlers, then override providers.
	SetupRoutesV2(app, cfg, valSvc, activationSvc, adminSvc, pool, tenantStore, rateLimiter, licenseStore, signerRegistry, auditQuery, webhookEncKey, webhookRepo)
	// We need to rebind readiness on handler. Since V2 constructed a new handler internally and did not expose it,
	// we instead re-register /readyz with the provided functions.
	// Simpler: add a lightweight override endpoint.
	if isReady == nil && queueDepth == nil {
		return
	}
	// Replace /readyz with provider-backed version.
	h := handlers.NewHandler(cfg, valSvc, activationSvc, adminSvc, pool, nil, licenseStore, signerRegistry, auditQuery, webhookEncKey, webhookRepo)
	h.IsReady = func() bool {
		if isReady != nil {
			return isReady()
		}
		return true
	}
	h.QueueDepth = func() int {
		if queueDepth != nil {
			return queueDepth()
		}
		if pool != nil {
			return pool.QueueDepth()
		}
		return 0
	}
	app.All("/readyz", h.Ready)
}
