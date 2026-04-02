package main

import (
	"context"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
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

func initDeps(ctx context.Context, cfg *appConfig) (*deps, error) {
	appCfg := setup.Load()
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

	rl := middleware.NewRateLimiter()
	auditor := idb.NewAuditWriter(pool)
	adminSvc := app.NewAdminService(licenseRepo, tenantRepo, licenseStore, tenantStore, rl, auditor)

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
		},
	}, nil
}

