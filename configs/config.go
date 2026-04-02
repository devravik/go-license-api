package configs

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppName           string
	AppPort           string
	AdminKey          string
	AppMode           string
	AppEnv            string
	JSONEngine        string
	WorkerCount       int
	WorkerQueueSize   int
	WorkerTimeout     time.Duration
	ValidationTimeout time.Duration
	ClientTimeout     time.Duration
	MinLicenseKeyLen  int
	AuditWorkerCount  int
	AuditQueueSize    int
	AuditRetryCount   int
	AuditRetryDelay   time.Duration
	AdminAllowedCIDRs []string
	WebhookEncKeyHex  string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	cfg := &Config{
		AppName:           getEnv("APP_NAME", "Go License API"),
		AppPort:           getEnv("APP_PORT", "8080"),
		AdminKey:          getEnv("ADMIN_API_KEY", ""),
		AppMode:           getEnv("APP_MODE", "single"),
		AppEnv:            getEnv("APP_ENV", "develop"),
		JSONEngine:        getEnv("JSON_ENGINE", "std"),
		WorkerCount:       getEnvInt("WORKER_COUNT", 8),
		WorkerQueueSize:   getEnvInt("WORKER_QUEUE_SIZE", 500),
		WorkerTimeout:     getEnvDuration("WORKER_TIMEOUT", 1500*time.Millisecond),
		ValidationTimeout: getEnvDuration("VALIDATION_TIMEOUT", 2*time.Second),
		ClientTimeout:     getEnvDuration("CLIENT_TIMEOUT", 3*time.Second),
		MinLicenseKeyLen:  getEnvInt("MIN_LICENSE_KEY_LEN", 8),
		AuditWorkerCount:  getEnvInt("AUDIT_WORKER_COUNT", 2),
		AuditQueueSize:    getEnvInt("AUDIT_QUEUE_SIZE", getEnvInt("WORKER_COUNT", 8)*100),
		AuditRetryCount:   getEnvInt("AUDIT_RETRY_COUNT", 1),
		AuditRetryDelay:   getEnvDuration("AUDIT_RETRY_DELAY", 50*time.Millisecond),
		AdminAllowedCIDRs: getEnvCSV("ADMIN_ALLOWED_CIDRS"),
		WebhookEncKeyHex:  getEnv("WEBHOOK_ENCRYPTION_KEY", ""),
	}
	if cfg.WorkerTimeout > cfg.ValidationTimeout {
		cfg.WorkerTimeout = cfg.ValidationTimeout
	}
	if cfg.ValidationTimeout > cfg.ClientTimeout {
		cfg.ValidationTimeout = cfg.ClientTimeout
	}
	if cfg.MinLicenseKeyLen < 1 {
		cfg.MinLicenseKeyLen = 1
	}
	if cfg.AuditQueueSize < cfg.WorkerCount*100 {
		cfg.AuditQueueSize = cfg.WorkerCount * 100
	}
	if strings.TrimSpace(cfg.AdminKey) == "" {
		log.Fatal("ADMIN_API_KEY is required")
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fallback
		}
		return b
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := getEnv(key, "")
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := getEnv(key, "")
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getEnvCSV(key string) []string {
	v := getEnv(key, "")
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
