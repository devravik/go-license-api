package server

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"crypto/ed25519"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/audit"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http"
	"github.com/devravik/go-license-api/internal/http/middleware"
	iaudit "github.com/devravik/go-license-api/internal/infrastructure/audit"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	ilock "github.com/devravik/go-license-api/internal/infrastructure/lock"
	icrypto "github.com/devravik/go-license-api/internal/security"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/devravik/go-license-api/internal/webhook"
	"github.com/devravik/go-license-api/internal/worker"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jackc/pgx/v5/pgxpool"
	jsoniter "github.com/json-iterator/go"
)

// Server wires all runtime dependencies and exposes lifecycle hooks.
type Server struct {
	app         *fiber.App
	cfg         *setup.Config
	db          *pgxpool.Pool
	pool        *worker.Pool
	poolCtx     context.Context
	poolCancel  context.CancelFunc
	auditWriter *idb.AuditWriter
	auditQueue  chan *domain.AuditEntry
	auditWorker *iaudit.Worker
}

func New() (*Server, *setup.Config) {
	cfg := setup.Load()
	logCfg := setup.LoadLoggingConfig()
	cacheCfg := setup.LoadCacheConfig()
	limiterCfg := setup.LoadLimiterConfig()

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

	// DB pool sizing: all goroutines in this process share one pgxpool, so
	// size it for peak concurrency (admin writes + activation transactions).
	// Activation uses SELECT FOR UPDATE + INSERT in a transaction; budget ~1
	// connection per expected concurrent write at peak (5 % of workers).
	// Default is deliberately conservative to stay within Postgres' default
	// max_connections=100 when multiple app instances run alongside other
	// clients.  Override via DB_MAX_CONNS env var (see setup.LoadDatabaseConfig).
	const defaultMaxConns int32 = 20
	limits := idb.PoolLimits{
		MaxConns:        defaultMaxConns,
		MinConns:        2,
		MaxConnLifetime: 5 * time.Minute,
		MaxConnIdleTime: 1 * time.Minute,
	}
	log.Printf("event=db_pool config max_conns=%d min_conns=%d max_lifetime=%s max_idle=%s",
		limits.MaxConns, limits.MinConns, limits.MaxConnLifetime, limits.MaxConnIdleTime)
	pool, err := idb.ConnectWithLimits(ctx, databaseURL, limits)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}

	licenseRepo := idb.NewLicenseRepo(pool)
	tenantRepo := idb.NewTenantRepo(pool)
	productRepo := idb.NewProductRepo(pool)
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

			// Log detailed error server-side with request context; do not leak to clients.
			reqID := strings.TrimSpace(c.Get("X-Request-ID"))
			if reqID == "" {
				reqID = strings.TrimSpace(c.GetRespHeader("X-Request-ID"))
			}
			log.Printf("event=http_error path=%s method=%s status=%d request_id=%s err=%v",
				c.Path(), c.Method(), code, reqID, err)

			// Return a safe, generic error response.
			// Keep the minimal schema aligned with validation responses that may check `valid`.
			return c.Status(code).JSON(fiber.Map{
				"valid":      false,
				"error":      "internal_error",
				"request_id": reqID,
			})
		},
	}
	// Prefork is configured at Listen time (cmd/server) based on REDIS_URL.

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
	// Optional product cache: construct with existing cache TTLS to avoid new config surface.
	productL1, err := cache.NewL1Cache(cacheCfg.L1MaxEntries)
	if err != nil {
		log.Fatalf("init product l1: %v", err)
	}
	productStore := cache.NewProductStore(productL1, l2, cacheCfg.ProductTTL, cacheCfg.ProductTTLNegative)

	// Cross-instance invalidation listeners (self-reconnecting).
	if l2 != nil {
		licenseStore.SubscribeInvalidation(context.Background())
		tenantStore.SubscribeInvalidation(context.Background())
		// Subscribe to tenant created/updated events to keep cache fresh across processes.
		l2.Subscribe(context.Background(), "tenant:created", func(tenantID string) {
			// Control-plane read allowed in background to populate cache
			if t, err := tenantRepo.FindByID(context.Background(), tenantID); err == nil && t != nil {
				tenantStore.Set(context.Background(), t.ID, t.APIKey, t)
				if t.OldAPIKey != "" {
					tenantStore.Set(context.Background(), t.ID, t.OldAPIKey, t)
				}
			}
		})
		l2.Subscribe(context.Background(), "tenant:updated", func(tenantID string) {
			// Best-effort refresh: fetch and overwrite; if fetch fails, invalidate by tenant ID
			if t, err := tenantRepo.FindByID(context.Background(), tenantID); err == nil && t != nil {
				tenantStore.Set(context.Background(), t.ID, t.APIKey, t)
				if t.OldAPIKey != "" {
					tenantStore.Set(context.Background(), t.ID, t.OldAPIKey, t)
				}
			} else {
				tenantStore.InvalidateByTenantID(context.Background(), tenantID)
			}
		})
		// Subscribe to product events: upsert -> refresh; delete -> invalidate
		l2.Subscribe(context.Background(), "product:upsert", func(payload string) {
			// payload format: tenantID|code
			parts := strings.SplitN(payload, "|", 2)
			if len(parts) != 2 {
				return
			}
			tenantID, code := parts[0], parts[1]
			if p, err := productRepo.FindByCode(context.Background(), tenantID, code); err == nil && p != nil {
				productStore.Set(context.Background(), tenantID, code, p)
			}
		})
		l2.Subscribe(context.Background(), "product:delete", func(payload string) {
			parts := strings.SplitN(payload, "|", 2)
			if len(parts) != 2 {
				return
			}
			tenantID, code := parts[0], parts[1]
			productStore.Invalidate(context.Background(), tenantID, code)
		})
	}

	// Bounded warm-up at startup (DB allowed here).
	wctx, wcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer wcancel()
	if err := licenseStore.WarmUp(wctx, licenseRepo, cacheCfg.WarmUpLicenseLimit); err != nil {
		log.Printf("event=startup component=cache_warmup target=license status=error err=%v", err)
	} else {
		log.Printf("event=startup component=cache_warmup target=license status=success limit=%d l1_len=%d", cacheCfg.WarmUpLicenseLimit, licenseL1.Len())
	}
	// Fetch tenants once for reuse across tenant and product warmups.
	var allTenants []*domain.Tenant
	var tenantsErr error
	if cacheCfg.WarmUpTenantLimit > 0 || cacheCfg.WarmUpProductLimit > 0 {
		allTenants, tenantsErr = tenantRepo.FindAll(wctx)
		if tenantsErr != nil {
			log.Printf("event=startup component=cache_warmup target=tenant_fetch status=error err=%v", tenantsErr)
		}
	}
	// Tenant warmup (bounded). Tenants currently lack an updated_at column; we warm up to limit from cached list.
	if cacheCfg.WarmUpTenantLimit > 0 && tenantsErr == nil {
		warmed := 0
		for _, t := range allTenants {
			if warmed >= cacheCfg.WarmUpTenantLimit {
				break
			}
			// Only warm up active, non-suspended tenants.
			if t.IsSuspended() {
				continue
			}
			tenantStore.Set(wctx, t.ID, t.APIKey, t)
			if t.OldAPIKey != "" {
				tenantStore.Set(wctx, t.ID, t.OldAPIKey, t)
			}
			warmed++
		}
		log.Printf("event=startup component=cache_warmup target=tenant status=success tenants=%d l1_len=%d", warmed, tenantL1.Len())
	}
	// Product warmup (bounded): prioritize recently updated per tenant until cap is reached.
	if cacheCfg.WarmUpProductLimit > 0 && tenantsErr == nil {
		after := time.Now().Add(-30 * 24 * time.Hour) // last 30 days heuristic
		warmed := 0
		for _, t := range allTenants {
			if warmed >= cacheCfg.WarmUpProductLimit {
				break
			}
			plist, perr := productRepo.ListUpdatedAfter(wctx, t.ID, after)
			if perr != nil {
				continue
			}
			for _, p := range plist {
				// Only warm up active products.
				if !p.IsActive {
					continue
				}
				productStore.Set(wctx, t.ID, p.Code, p)
				warmed++
				if warmed >= cacheCfg.WarmUpProductLimit {
					break
				}
			}
		}
		log.Printf("event=startup component=cache_warmup target=product status=success products=%d", warmed)
	}

	rateLimiter := middleware.NewRateLimiter()
	failLimiter := middleware.NewAdaptiveFailLimiter(limiterCfg, cacheCfg.RedisURL)
	auditWriter := idb.NewAuditWriter(pool)
	auditCh := make(chan *domain.AuditEntry, cfg.AuditQueueSize)
	auditWorker := iaudit.NewWorker(auditWriter, auditCh, cfg.AuditWorkerCount, cfg.AuditRetryCount, cfg.AuditRetryDelay)
	asyncAuditWriter := iaudit.NewAsyncWriter(auditCh)
	awCtx, awCancel := context.WithCancel(context.Background())
	_ = awCancel // retained for potential future use; worker stops via ctx or closed queue
	auditWorker.Start(awCtx)

	valSvc := app.NewValidationService(tenantStore, licenseStore, licenseRepo, licenseStore, auditCh, cfg.MinLicenseKeyLen)
	activationLock := ilock.NewActivationLock()
	activationSvc := app.NewActivationService(licenseStore, licenseRepo, licenseStore, activationRepo, asyncAuditWriter, activationLock)

	adminSvc := app.NewAdminService(licenseRepo, tenantRepo, productRepo, licenseStore, tenantStore, productStore, rateLimiter, asyncAuditWriter)
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
	http.SetupRoutesV3(appInstance, cfg, valSvc, activationSvc, adminSvc, poolSvc, tenantStore, rateLimiter, licenseStore, signerRegistry, auditQuery, webhookEncKey, webhookRepo, failLimiter,
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
