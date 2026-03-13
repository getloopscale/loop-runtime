package db

import (
	"fmt"
	"os"
	"strconv"
)

// DatabaseConfig holds the PostgreSQL connection parameters.
type DatabaseConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

// GetDatabaseConfig returns database configuration from environment variables with sensible defaults.
func GetDatabaseConfig() DatabaseConfig {
	port := 5432
	if p, err := strconv.Atoi(getEnvOrDefault("DATABASE_PORT", "5432")); err == nil {
		port = p
	}

	return DatabaseConfig{
		Host:     getEnvOrDefault("DATABASE_HOST", "localhost"),
		Port:     port,
		Database: getEnvOrDefault("DATABASE_NAME", "loopzero"),
		User:     getEnvOrDefault("DATABASE_USER", "loopzero"),
		Password: getEnvOrDefault("DATABASE_PASSWORD", ""),
	}
}

// DSN returns the PostgreSQL connection string.
func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.User, c.Password, c.Host, c.Port, c.Database)
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
