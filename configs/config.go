package configs

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	AppName    string
	AppPort    string
	AdminKey   string
	AppMode    string
	AppEnv     string
	JSONEngine string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	return &Config{
		AppName:    getEnv("APP_NAME", "Go License API"),
		AppPort:    getEnv("APP_PORT", "8080"),
		AdminKey:   getEnv("ADMIN_API_KEY", "secret-admin-key"),
		AppMode:    getEnv("APP_MODE", "single"),
		AppEnv:     getEnv("APP_ENV", "develop"),
		JSONEngine: getEnv("JSON_ENGINE", "std"),
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
