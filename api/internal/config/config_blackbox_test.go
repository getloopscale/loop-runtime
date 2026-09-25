// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config_test

import (
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/config"
)

// LOOPZERO_CLUSTER_ISSUER is seeded for every test in this binary by TestMain
// in config_test.go.

func TestConfig_Load_DefaultValues(t *testing.T) {
	// Act
	cfg := config.Load()

	// Assert: server defaults
	if cfg.Port != "3001" {
		t.Errorf("expected default port '3001', got %q", cfg.Port)
	}
	if cfg.MetricsPort != "9090" {
		t.Errorf("expected default metrics port '9090', got %q", cfg.MetricsPort)
	}
	if cfg.Env != "development" {
		t.Errorf("expected default env 'development', got %q", cfg.Env)
	}
}

func TestConfig_Load_PositiveTimeouts(t *testing.T) {
	cfg := config.Load()

	timeouts := map[string]time.Duration{
		"ReadHeaderTimeout":    cfg.ReadHeaderTimeout,
		"ReadTimeout":          cfg.ReadTimeout,
		"IdleTimeout":          cfg.IdleTimeout,
		"GracefulShutdownTime": cfg.GracefulShutdownTime,
		"HTTPClientTimeout":    cfg.HTTPClientTimeout,
		"ArgocdClientTimeout":  cfg.ArgocdClientTimeout,
		"DBMaxConnIdleTime":    cfg.DBMaxConnIdleTime,
		"DBMaxConnLifetime":    cfg.DBMaxConnLifetime,
		"DBHealthCheckTime":    cfg.DBHealthCheckTime,
		"DBConnectTimeout":     cfg.DBConnectTimeout,
		"CBRetryInterval":      cfg.CBRetryInterval,
		"ArgocdPollInterval":   cfg.ArgocdPollInterval,
		"SSEHeartbeatInterval": cfg.SSEHeartbeatInterval,
		"SSEPollInterval":      cfg.SSEPollInterval,
		"SSESafetyTimeout":     cfg.SSESafetyTimeout,
		"AuthCacheTTL":         cfg.AuthCacheTTL,
		"BuildQueueTimeout":    cfg.BuildQueueTimeout,
		"BuildRunningTimeout":  cfg.BuildRunningTimeout,
		"BuildCancelTimeout":   cfg.BuildCancelTimeout,
		"BuildReaperInterval":  cfg.BuildReaperInterval,
	}

	for name, d := range timeouts {
		if d <= 0 {
			t.Errorf("expected positive duration for %s, got %v", name, d)
		}
	}
}

func TestConfig_Load_PositivePoolSizes(t *testing.T) {
	cfg := config.Load()

	if cfg.DBMaxConns <= 0 {
		t.Errorf("expected positive DBMaxConns, got %d", cfg.DBMaxConns)
	}
	if cfg.DBMinConns <= 0 {
		t.Errorf("expected positive DBMinConns, got %d", cfg.DBMinConns)
	}
	if cfg.DBMinConns > cfg.DBMaxConns {
		t.Errorf("DBMinConns (%d) should not exceed DBMaxConns (%d)", cfg.DBMinConns, cfg.DBMaxConns)
	}
	if cfg.SSEMaxConcurrentStreams <= 0 {
		t.Errorf("expected positive SSEMaxConcurrentStreams, got %d", cfg.SSEMaxConcurrentStreams)
	}
}

func TestConfig_Load_CircuitBreakerDefaults(t *testing.T) {
	cfg := config.Load()

	if cfg.CBKey != "argocd" {
		t.Errorf("expected CB key 'argocd', got %q", cfg.CBKey)
	}
	if cfg.CBOpenThreshold <= 0 {
		t.Errorf("expected positive CB open threshold, got %d", cfg.CBOpenThreshold)
	}
	if cfg.CBCloseThreshold <= 0 {
		t.Errorf("expected positive CB close threshold, got %d", cfg.CBCloseThreshold)
	}
	if cfg.CBRetryInterval <= 0 {
		t.Errorf("expected positive CB retry interval, got %v", cfg.CBRetryInterval)
	}
}

func TestConfig_Load_SourceUploadDefaults(t *testing.T) {
	cfg := config.Load()

	if cfg.MaxBundleSize <= 0 {
		t.Errorf("expected positive MaxBundleSize, got %d", cfg.MaxBundleSize)
	}
	if cfg.MultipartMaxMemory <= 0 {
		t.Errorf("expected positive MultipartMaxMemory, got %d", cfg.MultipartMaxMemory)
	}
}

func TestConfig_EnvOverride_Port(t *testing.T) {
	// Arrange
	t.Setenv("PORT", "8080")

	// Act
	cfg := config.Load()

	// Assert
	if cfg.Port != "8080" {
		t.Errorf("expected port '8080' from env var, got %q", cfg.Port)
	}
}

func TestConfig_EnvOverride_Duration(t *testing.T) {
	// Arrange
	t.Setenv("SERVER_READ_TIMEOUT", "45s")

	// Act
	cfg := config.Load()

	// Assert
	if cfg.ReadTimeout != 45*time.Second {
		t.Errorf("expected read timeout 45s, got %v", cfg.ReadTimeout)
	}
}

func TestConfig_EnvOverride_InvalidDuration_UsesDefault(t *testing.T) {
	// Arrange
	t.Setenv("SERVER_READ_TIMEOUT", "not-a-duration")

	// Act
	cfg := config.Load()

	// Assert: should fall back to default (30s)
	if cfg.ReadTimeout != 30*time.Second {
		t.Errorf("expected default read timeout 30s for invalid input, got %v", cfg.ReadTimeout)
	}
}

func TestConfig_EnvOverride_Int(t *testing.T) {
	// Arrange
	t.Setenv("DB_MAX_CONNS", "50")

	// Act
	cfg := config.Load()

	// Assert
	if cfg.DBMaxConns != 50 {
		t.Errorf("expected DBMaxConns 50, got %d", cfg.DBMaxConns)
	}
}

func TestConfig_EnvOverride_InvalidInt_UsesDefault(t *testing.T) {
	// Arrange
	t.Setenv("DB_MAX_CONNS", "not-a-number")

	// Act
	cfg := config.Load()

	// Assert: should fall back to default (25)
	if cfg.DBMaxConns != 25 {
		t.Errorf("expected default DBMaxConns 25 for invalid input, got %d", cfg.DBMaxConns)
	}
}

func TestConfig_EnvOverride_ArgocdInsecure_True(t *testing.T) {
	// Arrange
	t.Setenv("ARGOCD_INSECURE", "true")

	// Act
	cfg := config.Load()

	// Assert
	if !cfg.ArgocdInsecure {
		t.Error("expected ArgocdInsecure=true when ARGOCD_INSECURE=true")
	}
}

func TestConfig_EnvOverride_ArgocdInsecure_False(t *testing.T) {
	// Arrange
	t.Setenv("ARGOCD_INSECURE", "false")

	// Act
	cfg := config.Load()

	// Assert
	if cfg.ArgocdInsecure {
		t.Error("expected ArgocdInsecure=false when ARGOCD_INSECURE=false")
	}
}

func TestConfig_EnvOverride_ArgocdInsecure_DefaultFalse(t *testing.T) {
	// Act (no env var set)
	cfg := config.Load()

	// Assert
	if cfg.ArgocdInsecure {
		t.Error("expected ArgocdInsecure=false by default")
	}
}

func TestConfig_IsProduction_Development(t *testing.T) {
	cfg := config.Load()
	if cfg.IsProduction() {
		t.Error("expected IsProduction()=false for development env")
	}
}

func TestConfig_IsProduction_Production(t *testing.T) {
	t.Setenv("LOOPZERO_ENV", "production")
	cfg := config.Load()
	if !cfg.IsProduction() {
		t.Error("expected IsProduction()=true when LOOPZERO_ENV=production")
	}
}

func TestConfig_GiteaCrossCluster_FallsBackToGiteaURL(t *testing.T) {
	// Arrange: no cross-cluster URL set
	cfg := config.Load()

	// Act
	url := cfg.GiteaCrossCluster()

	// Assert: should fall back to GiteaURL
	if url != cfg.GiteaURL {
		t.Errorf("expected fallback to GiteaURL %q, got %q", cfg.GiteaURL, url)
	}
}

func TestConfig_GiteaCrossCluster_UsesExplicitURL(t *testing.T) {
	// Arrange
	t.Setenv("GITEA_CROSS_CLUSTER_URL", "http://10.0.1.3:30000")

	// Act
	cfg := config.Load()
	url := cfg.GiteaCrossCluster()

	// Assert
	if url != "http://10.0.1.3:30000" {
		t.Errorf("expected cross-cluster URL, got %q", url)
	}
}

func TestConfig_Validate_DefaultSecret_NoError(t *testing.T) {
	// Validate should not panic with default config
	cfg := config.Load()

	// Act: should log warning but not panic
	_ = cfg.Validate()
}

func TestConfig_Validate_RegistryInsecure_BlockedInProduction(t *testing.T) {
	// Arrange: production + REGISTRY_INSECURE=true → must return error
	t.Setenv("LOOPZERO_ENV", "production")
	t.Setenv("REGISTRY_INSECURE", "true")
	// SEC-001: encryption key required in production — set a valid 32-byte hex key
	t.Setenv("LOOPZERO_SECRET_ENCRYPTION_KEY", "REDACTED_SECRET_010")

	cfg := config.Load()

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected error for REGISTRY_INSECURE=true in production, got nil")
	}
	if !contains(err.Error(), "REGISTRY_INSECURE") {
		t.Errorf("expected error mentioning REGISTRY_INSECURE, got %q", err.Error())
	}
}

func TestConfig_Validate_RegistryInsecure_AllowedInDevelopment(t *testing.T) {
	// Arrange: development + REGISTRY_INSECURE=true → no error
	t.Setenv("LOOPZERO_ENV", "development")
	t.Setenv("REGISTRY_INSECURE", "true")

	cfg := config.Load()

	// Act
	err := cfg.Validate()

	// Assert
	if err != nil {
		t.Errorf("expected no error for REGISTRY_INSECURE=true in development, got %v", err)
	}
}

func TestConfig_Validate_RegistryInsecure_FalseInProduction_OK(t *testing.T) {
	// Arrange: production + REGISTRY_INSECURE=false → no error (from this check)
	t.Setenv("LOOPZERO_ENV", "production")
	t.Setenv("REGISTRY_INSECURE", "false")
	// Also fix ArgoCD insecure to avoid that check failing
	t.Setenv("ARGOCD_INSECURE", "false")
	// SEC-001: encryption key required in production
	t.Setenv("LOOPZERO_SECRET_ENCRYPTION_KEY", "REDACTED_SECRET_010")

	cfg := config.Load()

	// Act
	err := cfg.Validate()

	// Assert: should pass (no registry insecure error)
	if err != nil && contains(err.Error(), "REGISTRY_INSECURE") {
		t.Errorf("unexpected REGISTRY_INSECURE error when flag is false: %v", err)
	}
}

// contains checks if s contains substr (helper to avoid strings import in test).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConfig_Validate_ProductionSecret(t *testing.T) {
	// Arrange: non-default secret
	t.Setenv("LOOPZERO_ARGO_CALLBACK_SECRET", "super-secret-production-key")

	cfg := config.Load()

	// Act: should not warn
	_ = cfg.Validate()

	// Assert: callback secret is set
	if cfg.ArgoCallbackSecret != "super-secret-production-key" {
		t.Errorf("expected production secret, got %q", cfg.ArgoCallbackSecret)
	}
}
