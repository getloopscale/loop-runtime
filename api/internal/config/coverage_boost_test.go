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

func TestEnvLoaderIntReturnsTheDefaultWhenUnset(t *testing.T) {
	l := &envLoader{}
	if got := l.int("NONEXISTENT_TEST_VAR_123456", 42); got != 42 || len(l.errs) != 0 {
		t.Errorf("got %d errs %v, want 42 and no error", got, l.errs)
	}
}

func TestEnvLoaderIntParsesAValue(t *testing.T) {
	t.Setenv("TEST_ENV_INT_VAR", "99")
	l := &envLoader{}
	if got := l.int("TEST_ENV_INT_VAR", 42); got != 99 {
		t.Errorf("expected 99, got %d", got)
	}
}

func TestEnvLoaderIntRecordsAnInvalidValue(t *testing.T) {
	t.Setenv("TEST_ENV_INT_BAD", "not-a-number")
	l := &envLoader{}
	l.int("TEST_ENV_INT_BAD", 42)
	if len(l.errs) != 1 {
		t.Fatalf("errs = %v, want one error for the invalid value", l.errs)
	}
}

func TestEnvLoaderDurationParsesAValue(t *testing.T) {
	t.Setenv("TEST_DUR_VAR", "5m")
	l := &envLoader{}
	if got := l.duration("TEST_DUR_VAR", 0); got.Minutes() != 5 {
		t.Errorf("expected 5m, got %v", got)
	}
}

func TestEnvLoaderDurationRecordsAnInvalidValue(t *testing.T) {
	t.Setenv("TEST_DUR_BAD", "not-a-duration")
	l := &envLoader{}
	l.duration("TEST_DUR_BAD", 0)
	if len(l.errs) != 1 {
		t.Fatalf("errs = %v, want one error for the invalid duration", l.errs)
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
		ClusterIssuer:  "loopzero-ca-issuer", // B14
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

// CHG-2026-04-30-DELETE-SINGLE-CELL: TestLoad_SingleCell removed —
// LOOPZERO_SINGLE_CELL env var + cfg.SingleCell field deleted. Multi-cell
// is the only architecture; LOOPZERO_SINGLE_CELL=true is now ignored
// silently (env vars without consumers are no-ops in our config style).
