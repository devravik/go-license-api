package server

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/http"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	jsoniter "github.com/json-iterator/go"
)

func New() (*fiber.App, *configs.Config) {
	cfg := configs.Load()
	logCfg := configs.LoadLoggingConfig()
	cacheCfg := configs.LoadCacheConfig()

	// Fail fast on startup if PostgreSQL isn't reachable.
	dbCfg := configs.LoadDatabaseConfig()
	databaseURL, err := dbCfg.BuildDatabaseURL()
	if err != nil {
		log.Fatalf("build database url: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := idb.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}

	licenseRepo := idb.NewLicenseRepo(pool)
	tenantRepo := idb.NewTenantRepo(pool)
	_ = idb.NewActivationRepo(pool)

	fiberCfg := fiber.Config{
		AppName:      cfg.AppName,
		ServerHeader: "",

		// JSON handling
		JSONEncoder: json.Marshal,
		JSONDecoder: json.Unmarshal,

		// Server-level timeouts (Network/TCP Layer)
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,

		// Body limits
		BodyLimit: 1024 * 1024,

		// Performance tuning
		DisableKeepalive:  false,
		ReduceMemoryUsage: true,

		// Error handling
		ErrorHandler: func(c fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError

			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			return c.Status(code).JSON(fiber.Map{
				"valid": false,
				"error": err.Error(),
			})
		},
	}

	if strings.EqualFold(cfg.JSONEngine, "jsoniter") {
		var json = jsoniter.ConfigFastest
		fiberCfg.JSONEncoder = json.Marshal
		fiberCfg.JSONDecoder = json.Unmarshal
	}

	appInstance := fiber.New(fiberCfg)

	// Middleware
	appInstance.Use(requestid.New())
	logCfg.Setup(appInstance)
	appInstance.Use(recover.New())

	// Initialize services
	licenseL1, err := cache.NewL1Cache(cacheCfg.L1MaxEntries)
	if err != nil {
		log.Fatalf("init license l1: %v", err)
	}
	tenantL1, err := cache.NewL1Cache(cacheCfg.L1MaxEntries)
	if err != nil {
		log.Fatalf("init tenant l1: %v", err)
	}

	var l2 *cache.L2Cache
	if cacheCfg.RedisURL != "" {
		l2, err = cache.NewL2Cache(cacheCfg.RedisURL)
		if err != nil {
			log.Fatalf("init redis l2: %v", err)
		}
	}

	licenseStore := cache.NewLicenseStore(licenseL1, l2, cacheCfg.LicenseTTLL1, cacheCfg.LicenseTTLL2, cacheCfg.LicenseTTLActive, cacheCfg.LicenseTTLNegative)
	tenantStore := cache.NewTenantStore(tenantL1, l2, cacheCfg.TenantTTL, cacheCfg.TenantTTLNegative)

	// Cross-instance invalidation listeners (self-reconnecting).
	if l2 != nil {
		licenseStore.SubscribeInvalidation(context.Background())
		tenantStore.SubscribeInvalidation(context.Background())
	}

	// Bounded warm-up at startup (DB allowed here).
	wctx, wcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wcancel()
	if err := licenseStore.WarmUp(wctx, licenseRepo, cacheCfg.WarmUpLicenseLimit); err != nil {
		log.Printf("license warmup failed: %v", err)
	}
	// Tenant warmup (bounded). Tenants currently lack an updated_at column; we warm up to limit from FindAll.
	if cacheCfg.WarmUpTenantLimit > 0 {
		tenants, err := tenantRepo.FindAll(wctx)
		if err != nil {
			log.Printf("tenant warmup fetch failed: %v", err)
		} else {
			n := cacheCfg.WarmUpTenantLimit
			if n > len(tenants) {
				n = len(tenants)
			}
			for i := 0; i < n; i++ {
				t := tenants[i]
				tenantStore.Set(wctx, t.ID, t.APIKey, t)
				if t.OldAPIKey != "" {
					tenantStore.Set(wctx, t.ID, t.OldAPIKey, t)
				}
			}
		}
	}

	rateLimiter := middleware.NewRateLimiter()
	valSvc := app.NewValidationService(tenantStore, licenseStore)
	adminSvc := app.NewAdminService(licenseRepo, tenantRepo, licenseStore, tenantStore, rateLimiter)
	poolSvc := worker.NewPool(cfg.WorkerCount, cfg.WorkerQueueSize, valSvc)
	poolSvc.Start(context.Background())

	// Setup routes with injected config and services
	http.SetupRoutes(appInstance, cfg, valSvc, adminSvc, poolSvc, tenantStore, rateLimiter)

	return appInstance, cfg
}
