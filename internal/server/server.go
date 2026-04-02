package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/audit"
	iaudit "github.com/devravik/go-license-api/internal/infrastructure/audit"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	icrypto "github.com/devravik/go-license-api/internal/infrastructure/crypto"
	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	ilock "github.com/devravik/go-license-api/internal/infrastructure/lock"
	"github.com/devravik/go-license-api/internal/webhook"
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
	activationRepo := idb.NewActivationRepo(pool)

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
	auditWriter := idb.NewAuditWriter(pool)
	auditCh := make(chan *domain.AuditEntry, cfg.AuditQueueSize)
	auditWorker := iaudit.NewWorker(auditWriter, auditCh, cfg.AuditWorkerCount, cfg.AuditRetryCount, cfg.AuditRetryDelay)
	auditWorker.Start(context.Background())

	valSvc := app.NewValidationService(tenantStore, licenseStore, auditCh, cfg.MinLicenseKeyLen)
	activationLock := ilock.NewActivationLock()
	activationSvc := app.NewActivationService(licenseStore, activationRepo, auditWriter, activationLock)
	adminSvc := app.NewAdminService(licenseRepo, tenantRepo, licenseStore, tenantStore, rateLimiter, auditWriter)
	poolSvc := worker.NewPool(cfg.WorkerCount, cfg.WorkerQueueSize, valSvc, cfg.WorkerTimeout)
	poolSvc.Start(context.Background())

	// Audit query service (admin read path only)
	auditQuery := audit.NewQueryService(pool)

	// Initialize signing keys and registry (global keypair at startup).
	_, priv, err := icrypto.GenerateEd25519KeyPair()
	if err != nil {
		log.Fatalf("generate signing keypair: %v", err)
	}
	globalSigner := icrypto.NewEd25519Signer(priv, "global-1", cfg.AppName)
	signerRegistry := icrypto.NewSignerRegistry(globalSigner)

	// Initialize webhook dispatcher and cache (optional feature).
	var webhookEncKey []byte
	if strings.TrimSpace(cfg.WebhookEncKeyHex) != "" {
		dec, err := hex.DecodeString(cfg.WebhookEncKeyHex)
		if err != nil || len(dec) != 32 {
			log.Printf("invalid WEBHOOK_ENCRYPTION_KEY; webhooks disabled")
		} else {
			webhookEncKey = dec
		}
	}
	var dispatcher *webhook.Dispatcher
	if webhookEncKey != nil {
		dispatcher = webhook.NewDispatcher(pool, webhookEncKey)
		// best-effort cache load
		if err := dispatcher.LoadWebhooks(context.Background()); err != nil {
			log.Printf("webhook load failed: %v", err)
		}
	}
	webhookRepo := idb.NewWebhookRepo(pool)

	// Setup routes with injected config and services (extended)
	http.SetupRoutesV2(appInstance, cfg, valSvc, activationSvc, adminSvc, poolSvc, tenantStore, rateLimiter, licenseStore, signerRegistry, auditQuery, webhookEncKey, webhookRepo)

	return appInstance, cfg
}
