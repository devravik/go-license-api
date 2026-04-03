package setup

import "time"

type LimiterConfig struct {
	Enabled bool

	FailsPerMinute       int
	GlobalFailsPerMinute int

	BlockDurations []time.Duration
	StrikeTTL      time.Duration

	RedisEnabled bool
	RedisURL     string

	LocalCacheEnabled bool
	LocalBlockTTL     time.Duration

	FailOpen           bool
	TrackUnknownTenant bool
	KeyPrefix          string
	LogBlocks          bool
}

func LoadLimiterConfig() *LimiterConfig {
	block1 := time.Duration(getEnvInt("LIMITER_BLOCK_1", 5)) * time.Minute
	block2 := time.Duration(getEnvInt("LIMITER_BLOCK_2", 15)) * time.Minute
	block3 := time.Duration(getEnvInt("LIMITER_BLOCK_3", 60)) * time.Minute
	if block1 <= 0 {
		block1 = 5 * time.Minute
	}
	if block2 <= 0 {
		block2 = 15 * time.Minute
	}
	if block3 <= 0 {
		block3 = 60 * time.Minute
	}

	cfg := &LimiterConfig{
		Enabled: getEnvBool("LIMITER_ENABLED", true),

		FailsPerMinute:       getEnvInt("LIMITER_FAILS_PER_MINUTE", 20),
		GlobalFailsPerMinute: getEnvInt("LIMITER_GLOBAL_FAILS_PER_MINUTE", 100),

		BlockDurations: []time.Duration{block1, block2, block3},
		StrikeTTL:      time.Duration(getEnvInt("LIMITER_STRIKE_TTL_HOURS", 24)) * time.Hour,

		RedisEnabled: getEnvBool("LIMITER_REDIS_ENABLED", true),
		RedisURL:     getEnv("LIMITER_REDIS_URL", ""),

		LocalCacheEnabled: getEnvBool("LIMITER_LOCAL_CACHE_ENABLED", true),
		LocalBlockTTL:     time.Duration(getEnvInt("LIMITER_LOCAL_BLOCK_TTL_SECONDS", 30)) * time.Second,

		FailOpen:           getEnvBool("LIMITER_FAIL_OPEN", true),
		TrackUnknownTenant: getEnvBool("LIMITER_TRACK_UNKNOWN_TENANT", true),
		KeyPrefix:          getEnv("LIMITER_KEY_PREFIX", "limiter"),
		LogBlocks:          getEnvBool("LIMITER_LOG_BLOCKS", true),
	}
	if cfg.FailsPerMinute < 1 {
		cfg.FailsPerMinute = 20
	}
	if cfg.GlobalFailsPerMinute < 1 {
		cfg.GlobalFailsPerMinute = 100
	}
	if cfg.StrikeTTL < time.Hour {
		cfg.StrikeTTL = 24 * time.Hour
	}
	if cfg.LocalBlockTTL <= 0 {
		cfg.LocalBlockTTL = 30 * time.Second
	}
	return cfg
}
