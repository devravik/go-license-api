package main

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	ievents "github.com/devravik/go-license-api/internal/infrastructure/events"
	"github.com/devravik/go-license-api/internal/infrastructure/idgen"
	ilock "github.com/devravik/go-license-api/internal/infrastructure/lock"
	"github.com/devravik/go-license-api/internal/ports"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adminFacade struct {
	Admin app.AdminService
}

type repoFacade struct {
	Licenses domain.LicenseRepository
	Tenants  domain.TenantRepository
	Products domain.ProductRepository
	Plans    domain.PlanRepository
	DB       *pgxpool.Pool
}

type cacheFacade struct {
	LicenseStore *cache.LicenseStore
	TenantStore  *cache.TenantStore
	L2           *cache.L2Cache
}

type systemFacade struct {
	Config    *setup.Config
	CacheConf *setup.CacheConfig
}

type bizFacade struct {
	Validation app.ValidationService
	Activation app.ActivationService
}

func initDeps(ctx context.Context, cfg *appConfig) (*deps, error) {
	appCfg := setup.Load()
	idgen.ConfigureLength(appCfg.NanoIDLength)
	cacheCfg := setup.LoadCacheConfig()
	dbCfg := setup.LoadDatabaseConfig()

	dsn, err := dbCfg.BuildDatabaseURL()
	if err != nil {
		return nil, err
	}
	dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dbCancel()
	pool, err := idb.Connect(dbCtx, dsn)
	if err != nil {
		return nil, err
	}

	licenseRepo := idb.NewLicenseRepo(pool)
	tenantRepo := idb.NewTenantRepo(pool)
	productRepo := idb.NewProductRepo(pool)
	planRepo := idb.NewPlanRepo(pool)
	activationRepo := idb.NewActivationRepo(pool)

	licenseL1, _ := cache.NewL1Cache(cacheCfg.L1MaxEntries)
	tenantL1, _ := cache.NewL1Cache(cacheCfg.L1MaxEntries)

	var l2 *cache.L2Cache
	if cacheCfg.RedisURL != "" {
		if c, err := cache.NewL2Cache(cacheCfg.RedisURL); err == nil {
			l2 = c
		}
	}

	licenseStore := cache.NewLicenseStore(licenseL1, l2, cacheCfg.LicenseTTLL1, cacheCfg.LicenseTTLL2, cacheCfg.LicenseTTLActive, cacheCfg.LicenseTTLNegative)
	tenantStore := cache.NewTenantStore(tenantL1, l2, cacheCfg.TenantTTL, cacheCfg.TenantTTLNegative)
	productL1, _ := cache.NewL1Cache(cacheCfg.L1MaxEntries)
	productStore := cache.NewProductStore(productL1, l2, cacheCfg.ProductTTL, cacheCfg.ProductTTLNegative)
	planL1, _ := cache.NewL1Cache(cacheCfg.L1MaxEntries)
	planStore := cache.NewPlanStore(planL1, l2, cacheCfg.PlanTTL, cacheCfg.PlanTTLNegative)

	rl := middleware.NewRateLimiter()
	auditor := idb.NewAuditWriter(pool)
	adminSvc := app.NewAdminService(licenseRepo, tenantRepo, productRepo, planRepo, licenseStore, tenantStore, productStore, planStore, rl, auditor)
	valSvc := app.NewValidationService(tenantStore, licenseStore, licenseRepo, licenseStore, nil, appCfg.MinLicenseKeyLen)
	actLock := ilock.NewActivationLock()
	actSvc := app.NewActivationService(licenseStore, licenseRepo, licenseStore, activationRepo, auditor, actLock)

	// Optional Event Publisher (Redis-backed)
	var publisher ports.EventPublisher
	if cacheCfg.RedisURL != "" {
		if p, err := ievents.NewRedisEventPublisher(cacheCfg.RedisURL); err == nil {
			publisher = p
		} else {
			publisher = ievents.NoopPublisher{}
		}
	} else {
		publisher = ievents.NoopPublisher{}
	}

	return &deps{
		Config: &appConfig{
			Pretty:  cfg.Pretty,
			Timeout: cfg.Timeout,
		},
		Services: &services{
			Admin: &adminFacade{Admin: adminSvc},
			Repo: &repoFacade{
				Licenses: licenseRepo,
				Tenants:  tenantRepo,
				Products: productRepo,
				Plans:    planRepo,
				DB:       pool,
			},
			Cache: &cacheFacade{
				LicenseStore: licenseStore,
				TenantStore:  tenantStore,
				L2:           l2,
			},
			Sys: &systemFacade{
				Config:    appCfg,
				CacheConf: cacheCfg,
			},
			Biz: &bizFacade{
				Validation: valSvc,
				Activation: actSvc,
			},
			Pub: publisher,
		},
	}, nil
}
