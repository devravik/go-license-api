package configs

import (
	"fmt"
	"log"
	"net/url"

	"github.com/joho/godotenv"
)

type DatabaseConfig struct {
	// DatabaseURL, when set, takes precedence over the DB_* fields.
	// Example: postgres://user:pass@host:5432/dbname?sslmode=disable
	DatabaseURL string

	Host     string
	Port     string
	Database string
	Username string
	Password string
}

// LoadDatabaseConfig reads DB settings from environment variables.
//
// Expected variables:
// - DATABASE_URL (optional; if set, overrides DB_* variables)
// - DB_HOST
// - DB_PORT
// - DB_DATABASE
// - DB_USERNAME
// - DB_PASSWORD
func LoadDatabaseConfig() *DatabaseConfig {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	return &DatabaseConfig{
		DatabaseURL: getEnv("DATABASE_URL", ""),
		Host:        getEnv("DB_HOST", "127.0.0.1"),
		Port:        getEnv("DB_PORT", "5432"),
		Database:    getEnv("DB_DATABASE", ""),
		Username:    getEnv("DB_USERNAME", ""),
		Password:    getEnv("DB_PASSWORD", ""),
	}
}

func (c *DatabaseConfig) BuildDatabaseURL() (string, error) {
	if c.DatabaseURL != "" {
		return c.DatabaseURL, nil
	}

	if c.Database == "" {
		return "", fmt.Errorf("DB_DATABASE is required")
	}
	if c.Username == "" {
		return "", fmt.Errorf("DB_USERNAME is required")
	}
	if c.Password == "" {
		return "", fmt.Errorf("DB_PASSWORD is required")
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.Username, c.Password), // URL-encodes special chars in password.
		Host:   fmt.Sprintf("%s:%s", c.Host, c.Port),
		Path:   c.Database,
	}
	return u.String(), nil
}
