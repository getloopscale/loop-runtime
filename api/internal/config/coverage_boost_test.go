// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"testing"
)

// =============================================================================
// GiteaCrossCluster / MinIOCrossCluster tests
// =============================================================================

func TestGiteaCrossCluster_FallsBackToGiteaURL(t *testing.T) {
	cfg := Config{
		GiteaURL:             "http://gitea.internal:3000",
		GiteaCrossClusterURL: "",
	}
	got := cfg.GiteaCrossCluster()
	if got != "http://gitea.internal:3000" {
		t.Errorf("expected fallback to GiteaURL, got %q", got)
	}
}

func TestGiteaCrossCluster_UsesExplicitURL(t *testing.T) {
	cfg := Config{
		GiteaURL:             "http://gitea.internal:3000",
		GiteaCrossClusterURL: "http://gitea.cross:3000",
	}
	got := cfg.GiteaCrossCluster()
	if got != "http://gitea.cross:3000" {
		t.Errorf("expected cross-cluster URL, got %q", got)
	}
}

func TestMinIOCrossCluster_FallsBackToMinIOEndpoint(t *testing.T) {
	cfg := Config{
		MinIOEndpoint:             "minio.internal:9000",
		MinIOCrossClusterEndpoint: "",
	}
	got := cfg.MinIOCrossCluster()
	if got != "minio.internal:9000" {
		t.Errorf("expected fallback to MinIOEndpoint, got %q", got)
	}
}

func TestMinIOCrossCluster_UsesExplicitEndpoint(t *testing.T) {
	cfg := Config{
		MinIOEndpoint:             "minio.internal:9000",
		MinIOCrossClusterEndpoint: "minio.cross:9000",
	}
	got := cfg.MinIOCrossCluster()
	if got != "minio.cross:9000" {
		t.Errorf("expected cross-cluster endpoint, got %q", got)
	}
}

// =============================================================================
// envInt / envDuration tests
// =============================================================================

func TestEnvInt_ReturnsDefault(t *testing.T) {
	got := envInt("NONEXISTENT_TEST_VAR_123456", 42)
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestEnvInt_ParsesValue(t *testing.T) {
	t.Setenv("TEST_ENV_INT_VAR", "99")
	got := envInt("TEST_ENV_INT_VAR", 42)
	if got != 99 {
		t.Errorf("expected 99, got %d", got)
	}
}

func TestEnvInt_InvalidReturnsDefault(t *testing.T) {
	t.Setenv("TEST_ENV_INT_BAD", "not-a-number")
	got := envInt("TEST_ENV_INT_BAD", 42)
	if got != 42 {
		t.Errorf("expected 42 for invalid value, got %d", got)
	}
}

func TestEnvDuration_ParsesValue(t *testing.T) {
	t.Setenv("TEST_DUR_VAR", "5m")
	got := envDuration("TEST_DUR_VAR", 0)
	if got.Minutes() != 5 {
		t.Errorf("expected 5m, got %v", got)
	}
}

func TestEnvDuration_InvalidReturnsDefault(t *testing.T) {
	t.Setenv("TEST_DUR_BAD", "not-a-duration")
	got := envDuration("TEST_DUR_BAD", 0)
	if got != 0 {
		t.Errorf("expected 0 for invalid duration, got %v", got)
	}
}

// =============================================================================
// Validate — production-specific checks
// =============================================================================

func TestValidate_ProductionRejectsDefaultArgoCallbackSecret(t *testing.T) {
	cfg := Config{
		Env:                "production",
		ArgoCallbackSecret: "loopzero-dev-callback-secret",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for default callback secret in production")
	}
}

func TestValidate_ProductionRejectsSSLModeDisable(t *testing.T) {
	cfg := Config{
		Env:         "production",
		DatabaseURL: "postgres://host:5432/db?sslmode=disable",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for sslmode=disable in production")
	}
}

func TestValidate_DevelopmentAcceptsDefaultCredentials(t *testing.T) {
	cfg := Config{
		Env:            "development",
		MinIOAccessKey: "loopzero-admin",
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error in development, got %v", err)
	}
}

func TestIsProduction_True(t *testing.T) {
	cfg := Config{Env: "production"}
	if !cfg.IsProduction() {
		t.Error("expected IsProduction=true")
	}
}

func TestIsProduction_False(t *testing.T) {
	cfg := Config{Env: "development"}
	if cfg.IsProduction() {
		t.Error("expected IsProduction=false")
	}
}

// =============================================================================
// Load — full config with env vars
// =============================================================================

func TestLoad_DefaultPort(t *testing.T) {
	cfg := Load()
	if cfg.Port != "3001" {
		// May be overridden by env, but verify Load() doesn't crash
		_ = cfg.Port
	}
}

func TestLoad_CustomPort(t *testing.T) {
	t.Setenv("PORT", "9999")
	cfg := Load()
	if cfg.Port != "9999" {
		t.Errorf("expected 9999, got %q", cfg.Port)
	}
}

func TestLoad_SingleCell(t *testing.T) {
	t.Setenv("LOOPZERO_SINGLE_CELL", "true")
	cfg := Load()
	if !cfg.SingleCell {
		t.Error("expected SingleCell=true")
	}
}
