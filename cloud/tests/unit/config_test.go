// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/config"
)

func TestConfigLoadDefaults(t *testing.T) {
	// Clear env to test defaults
	for _, key := range []string{"CLOUD_LISTEN_ADDR", "KRATOS_PUBLIC_URL", "HYDRA_PUBLIC_URL", "HYDRA_CLIENT_ID", "LOOPZERO_ENV"} {
		_ = os.Unsetenv(key)
	}

	cfg := config.Load()

	if cfg.ListenAddr != ":13002" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":13002")
	}
	if cfg.HydraClientID != "loopzero-cli" {
		t.Errorf("HydraClientID = %q, want %q", cfg.HydraClientID, "loopzero-cli")
	}
	if cfg.Environment != "dev" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "dev")
	}
}

func TestConfigLoadFromEnv(t *testing.T) {
	t.Setenv("CLOUD_LISTEN_ADDR", ":9999")
	t.Setenv("HYDRA_CLIENT_ID", "custom-cli")
	t.Setenv("LOOPZERO_ENV", "prod")

	cfg := config.Load()

	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9999")
	}
	if cfg.HydraClientID != "custom-cli" {
		t.Errorf("HydraClientID = %q, want %q", cfg.HydraClientID, "custom-cli")
	}
	if cfg.Environment != "prod" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "prod")
	}
}

func TestConfigValidateProd(t *testing.T) {
	t.Setenv("LOOPZERO_ENV", "prod")
	t.Setenv("HYDRA_PUBLIC_URL", "")

	cfg := config.Load()
	cfg.Environment = "prod"
	cfg.HydraPublicURL = ""

	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail when HYDRA_PUBLIC_URL is empty in prod")
	}
}

func TestConfigValidateDevOK(t *testing.T) {
	cfg := config.Load()
	cfg.Environment = "dev"

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should pass in dev: %v", err)
	}
}

func TestConfigDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/loopzerodb?sslmode=disable")
	cfg := config.Load()

	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/loopzerodb?sslmode=disable" {
		t.Errorf("DatabaseURL = %q, want postgres URL", cfg.DatabaseURL)
	}
}

func TestConfigDatabaseURLDefault(t *testing.T) {
	_ = os.Unsetenv("DATABASE_URL")
	cfg := config.Load()

	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL default should be empty, got %q", cfg.DatabaseURL)
	}
}

func TestConfigValidateProdRequiresDatabaseURL(t *testing.T) {
	cfg := config.Load()
	cfg.Environment = "prod"
	cfg.HydraPublicURL = "https://hydra.example.com"
	cfg.KratosPublicURL = "https://kratos.example.com"
	cfg.DatabaseURL = ""

	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail when DATABASE_URL is empty in prod")
	}
}

func TestConfigValidateProdRequiresStripeKeys(t *testing.T) {
	cfg := config.Load()
	cfg.Environment = "prod"
	cfg.HydraPublicURL = "https://hydra.example.com"
	cfg.KratosPublicURL = "https://kratos.example.com"
	cfg.DatabaseURL = "postgres://localhost/db"
	cfg.StripeAPIKey = ""
	cfg.StripeWebhookSecret = ""

	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail when Stripe keys are empty in prod")
	}
}

func TestConfigValidateProdPassesWithAllKeys(t *testing.T) {
	cfg := config.Load()
	cfg.Environment = "prod"
	cfg.HydraPublicURL = "https://hydra.example.com"
	cfg.KratosPublicURL = "https://kratos.example.com"
	cfg.DatabaseURL = "postgres://localhost/db"
	cfg.StripeAPIKey = "sk_live_xxx"
	cfg.StripeWebhookSecret = "whsec_xxx"
	// Set the secrets EXPLICITLY (>=32 chars) rather than relying on the dev
	// auto-gen from Load() — otherwise this test passes vacuously and would not
	// catch a regression in the prod secret requirements (T1.6).
	cfg.SessionTokenSecret = config.Secret("session-secret-explicit-32chars!!")
	cfg.ServiceAuthSecret = config.Secret("service-auth-secret-explicit-32c!")

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should pass with all prod keys set: %v", err)
	}
}

// T1.6 — production must require an explicit ServiceAuthSecret; a per-pod random
// secret would break multi-replica auth non-deterministically.
func TestConfigValidateProdRequiresServiceAuthSecret(t *testing.T) {
	cfg := config.Load()
	cfg.Environment = "prod"
	cfg.HydraPublicURL = "https://hydra.example.com"
	cfg.KratosPublicURL = "https://kratos.example.com"
	cfg.DatabaseURL = "postgres://localhost/db"
	cfg.StripeAPIKey = "sk_live_xxx"
	cfg.StripeWebhookSecret = "whsec_xxx"
	cfg.ServiceAuthSecret = "" // not set

	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail when LOOPZERO_SERVICE_AUTH_SECRET is empty in prod")
	}
}

// T1.6 — production must require the Kratos/Hydra admin URLs.
func TestConfigValidateProdRequiresAdminURLs(t *testing.T) {
	base := func() config.Config {
		c := config.Load()
		c.Environment = "prod"
		c.HydraPublicURL = "https://hydra.example.com"
		c.KratosPublicURL = "https://kratos.example.com"
		c.DatabaseURL = "postgres://localhost/db"
		c.StripeAPIKey = "sk_live_xxx"
		c.StripeWebhookSecret = "whsec_xxx"
		return c
	}

	c1 := base()
	c1.KratosAdminURL = ""
	if err := c1.Validate(); err == nil {
		t.Error("Validate() should fail when KRATOS_ADMIN_URL is empty in prod")
	}

	c2 := base()
	c2.HydraAdminURL = ""
	if err := c2.Validate(); err == nil {
		t.Error("Validate() should fail when HYDRA_ADMIN_URL is empty in prod")
	}
}

// T1.6 — secrets must NOT be auto-generated in production.
func TestConfigLoadProdDoesNotAutogenSecrets(t *testing.T) {
	t.Setenv("LOOPZERO_ENV", "prod")
	_ = os.Unsetenv("SESSION_TOKEN_SECRET")
	_ = os.Unsetenv("LOOPZERO_SERVICE_AUTH_SECRET")

	cfg := config.Load()

	if cfg.SessionTokenSecret.Value() != "" {
		t.Error("SESSION_TOKEN_SECRET must not be auto-generated in prod")
	}
	if cfg.ServiceAuthSecret.Value() != "" {
		t.Error("LOOPZERO_SERVICE_AUTH_SECRET must not be auto-generated in prod")
	}
}
