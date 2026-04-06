package setup

import (
	"time"
)

type CacheConfig struct {
	L1MaxEntries int

	LicenseTTLL1       time.Duration
	LicenseTTLL2       time.Duration
	LicenseTTLActive   time.Duration
	LicenseTTLNegative time.Duration

	TenantTTL          time.Duration
	TenantTTLNegative  time.Duration
	ProductTTL         time.Duration
	ProductTTLNegative time.Duration
	PlanTTL            time.Duration
	PlanTTLNegative    time.Duration

	RedisURL string

	WarmUpLicenseLimit int
	WarmUpTenantLimit  int
	WarmUpProductLimit int
	WarmUpPlanLimit    int
}

func LoadCacheConfig() *CacheConfig {
	return &CacheConfig{
		L1MaxEntries: getEnvInt("CACHE_L1_MAX_ENTRIES", 10000),

		LicenseTTLL1:       getEnvDuration("CACHE_LICENSE_TTL_L1", 5*time.Minute),
		LicenseTTLL2:       getEnvDuration("CACHE_LICENSE_TTL_L2", 72*time.Hour),
		LicenseTTLActive:   getEnvDuration("CACHE_LICENSE_TTL_ACTIVE", 24*time.Hour),
		LicenseTTLNegative: getEnvDuration("CACHE_LICENSE_TTL_NEGATIVE", 60*time.Second),

		// Tenants/products are small control-plane datasets; default to non-expiring in-memory entries.
		TenantTTL:         getEnvDuration("CACHE_TENANT_TTL", 0),
		TenantTTLNegative: getEnvDuration("CACHE_TENANT_TTL_NEGATIVE", 60*time.Second),
		ProductTTL:        getEnvDuration("CACHE_PRODUCT_TTL", 0),
		ProductTTLNegative: getEnvDuration("CACHE_PRODUCT_TTL_NEGATIVE",
			getEnvDuration("CACHE_LICENSE_TTL_NEGATIVE", 60*time.Second)),
		PlanTTL: getEnvDuration("CACHE_PLAN_TTL",
			getEnvDuration("CACHE_PRODUCT_TTL", 0)),
		PlanTTLNegative: getEnvDuration("CACHE_PLAN_TTL_NEGATIVE",
			getEnvDuration("CACHE_PRODUCT_TTL_NEGATIVE", 60*time.Second)),

		RedisURL: getEnv("REDIS_URL", ""),

		WarmUpLicenseLimit: getEnvInt("CACHE_WARMUP_LICENSE_LIMIT", 100000),
		WarmUpTenantLimit:  getEnvInt("CACHE_WARMUP_TENANT_LIMIT", 100),
		WarmUpProductLimit: getEnvInt("CACHE_WARMUP_PRODUCT_LIMIT", 10000),
		WarmUpPlanLimit:    getEnvInt("CACHE_WARMUP_PLAN_LIMIT", 10000),
	}
}
