package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/base64"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/devravik/go-license-api/internal/audit"
	iaudit "github.com/devravik/go-license-api/internal/infrastructure/audit"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	icrypto "github.com/devravik/go-license-api/internal/security"
	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	ilock "github.com/devravik/go-license-api/internal/infrastructure/lock"
	"github.com/devravik/go-license-api/internal/webhook"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"crypto/ed25519"
	"github.com/jackc/pgx/v5/pgxpool"
	jsoniter "github.com/json-iterator/go"
)

// Server wires all runtime dependencies and exposes lifecycle hooks.
type Server struct {
	app          *fiber.App
	cfg          *setup.Config
	db           *pgxpool.Pool
	pool         *worker.Pool
	poolCtx      context.Context
	poolCancel   context.CancelFunc
	auditWriter  *idb.AuditWriter
	auditQueue   chan *domain.AuditEntry
	auditWorker  *iaudit.Worker
}

func New() (*Server, *setup.Config) {
	cfg := setup.Load()
	logCfg := setup.LoadLoggingConfig()
	cacheCfg := setup.LoadCacheConfig()

	ready := atomic.Bool{}
	ready.Store(false)

	// Fail fast on startup if PostgreSQL isn't reachable.
	dbCfg := setup.LoadDatabaseConfig()
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
		log.Printf("event=startup component=cache_warmup target=license status=error err=%v", err)
	}
	// Tenant warmup (bounded). Tenants currently lack an updated_at column; we warm up to limit from FindAll.
	if cacheCfg.WarmUpTenantLimit > 0 {
		tenants, err := tenantRepo.FindAll(wctx)
		if err != nil {
			log.Printf("event=startup component=cache_warmup target=tenant status=error err=%v", err)
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
			log.Printf("event=startup component=cache_warmup target=tenant status=success tenants=%d", n)
		}
	}

	rateLimiter := middleware.NewRateLimiter()
	auditWriter := idb.NewAuditWriter(pool)
	auditCh := make(chan *domain.AuditEntry, cfg.AuditQueueSize)
	auditWorker := iaudit.NewWorker(auditWriter, auditCh, cfg.AuditWorkerCount, cfg.AuditRetryCount, cfg.AuditRetryDelay)
	awCtx, awCancel := context.WithCancel(context.Background())
	_ = awCancel // retained for potential future use; worker stops via ctx or closed queue
	auditWorker.Start(awCtx)

	valSvc := app.NewValidationService(tenantStore, licenseStore, auditCh, cfg.MinLicenseKeyLen)
	activationLock := ilock.NewActivationLock()
	activationSvc := app.NewActivationService(licenseStore, activationRepo, auditWriter, activationLock)
	adminSvc := app.NewAdminService(licenseRepo, tenantRepo, licenseStore, tenantStore, rateLimiter, auditWriter)
	poolSvc := worker.NewPool(cfg.WorkerCount, cfg.WorkerQueueSize, valSvc, cfg.WorkerTimeout)
	poolCtx, poolCancel := context.WithCancel(context.Background())
	poolSvc.Start(poolCtx)

	// Audit query service (admin read path only)
	auditQuery := audit.NewQueryService(pool)

	// Initialize signing keys and registry (global keypair at startup).
	var signerRegistry *icrypto.SignerRegistry
	if strings.TrimSpace(cfg.SigningKeyPath) != "" {
		keyData, err := os.ReadFile(cfg.SigningKeyPath)
		if err != nil {
			log.Fatalf("cannot read SIGNING_KEY_PATH: %v", err)
		}
		raw := strings.TrimSpace(string(keyData))
		dec, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			if hx, err2 := hex.DecodeString(raw); err2 == nil {
				dec = hx
			}
		}
		if len(dec) != ed25519.PrivateKeySize {
			log.Fatalf("SIGNING_KEY_PATH invalid key size: got=%d want=%d", len(dec), ed25519.PrivateKeySize)
		}
		priv := ed25519.PrivateKey(dec)
		globalSigner := icrypto.NewEd25519Signer(priv, "global-1", cfg.AppName)
		signerRegistry = icrypto.NewSignerRegistry(globalSigner)
	} else {
		_, priv, err := icrypto.GenerateEd25519KeyPair()
		if err != nil {
			log.Fatalf("generate signing keypair: %v", err)
		}
		globalSigner := icrypto.NewEd25519Signer(priv, "global-1", cfg.AppName)
		signerRegistry = icrypto.NewSignerRegistry(globalSigner)
	}

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
	http.SetupRoutesV3(appInstance, cfg, valSvc, activationSvc, adminSvc, poolSvc, tenantStore, rateLimiter, licenseStore, signerRegistry, auditQuery, webhookEncKey, webhookRepo,
		func() bool { return ready.Load() },
		func() int {
			if poolSvc != nil {
				return poolSvc.QueueDepth()
			}
			return 0
		})

	// Mark server ready after initialization completes.
	ready.Store(true)

	return &Server{
		app:         appInstance,
		cfg:         cfg,
		db:          pool,
		pool:        poolSvc,
		poolCtx:     poolCtx,
		poolCancel:  poolCancel,
		auditWriter: auditWriter,
		auditQueue:  auditCh,
		auditWorker: auditWorker,
	}, cfg
}

// Listen wraps Fiber's Listen with app-level configuration.
func (s *Server) Listen(addr string, cfg fiber.ListenConfig) error {
	return s.app.Listen(addr, cfg)
}

// Shutdown performs an ordered, timeout-aware shutdown.
func (s *Server) Shutdown(ctx context.Context) {
	// 1) Stop accepting HTTP connections
	log.Println("shutdown: stopping http server")
	if err := s.app.ShutdownWithContext(ctx); err != nil {
		log.Printf("shutdown: http server error: %v", err)
	}

	// 2) Stop accepting new jobs and drain workers with respect to timeout
	// Drain closes the internal queue and waits on worker waitgroup.
	log.Println("shutdown: draining workers")
	s.pool.Drain(ctx)

	// 3) Cancel worker context to stop any restart loops and exit goroutines
	s.poolCancel()

	// 4) Flush the audit writer with its own short timeout window
	log.Println("shutdown: flushing audit logs")
	timeout := s.cfg.AuditFlushTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.auditWriter.FlushWithContext(auditCtx)

	// 5) Close DB connection pool last
	log.Println("shutdown: closing database")
	s.db.Close()
	log.Println("shutdown: complete")
}
