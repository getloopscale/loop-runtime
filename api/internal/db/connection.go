// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
// SEC-006: sslmode is configurable via DATABASE_SSLMODE env var.
// MEET-2026-03-31-024: Default changed from "disable" to "require".
// Local dev must explicitly set DATABASE_SSLMODE=disable.
func (c DatabaseConfig) DSN() string {
	sslmode := getEnvOrDefault("DATABASE_SSLMODE", "require")
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User, c.Password, c.Host, c.Port, c.Database, sslmode)
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
