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
	AdminAllowedCIDRs []string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	return &Config{
		AppName:           getEnv("APP_NAME", "Go License API"),
		AppPort:           getEnv("APP_PORT", "8080"),
		AdminKey:          getEnv("ADMIN_API_KEY", "secret-admin-key"),
		AppMode:           getEnv("APP_MODE", "single"),
		AppEnv:            getEnv("APP_ENV", "develop"),
		JSONEngine:        getEnv("JSON_ENGINE", "std"),
		WorkerCount:       getEnvInt("WORKER_COUNT", 8),
		WorkerQueueSize:   getEnvInt("WORKER_QUEUE_SIZE", 500),
		AdminAllowedCIDRs: getEnvCSV("ADMIN_ALLOWED_CIDRS"),
	}
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
