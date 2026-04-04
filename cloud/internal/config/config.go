// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"fmt"
	"os"
)

// Secret is a string that redacts itself in logs, fmt output, and JSON serialization.
type Secret string

func (s Secret) String() string                { return "***" }
func (s Secret) GoString() string              { return "***" }
func (s Secret) MarshalJSON() ([]byte, error)  { return []byte(`"***"`), nil }
func (s Secret) MarshalText() ([]byte, error)  { return []byte("***"), nil }
func (s Secret) Value() string                 { return string(s) }

// Config holds all configuration for the cloud auth service.
// All fields are loaded from environment variables.
type Config struct {
	// ListenAddr is the address the cloud service listens on.
	// Env: CLOUD_LISTEN_ADDR (default: ":13002")
	ListenAddr string

	// KratosPublicURL is the Ory Kratos public API URL (user-facing flows).
	// Env: KRATOS_PUBLIC_URL (default: "http://ory-kratos-public.loopzero-system:80")
	KratosPublicURL string

	// KratosAdminURL is the Ory Kratos admin API URL (identity management).
	// Env: KRATOS_ADMIN_URL (default: "http://ory-kratos-admin.loopzero-system:80")
	KratosAdminURL string

	// HydraPublicURL is the Ory Hydra public API URL (OAuth2 endpoints).
	// Env: HYDRA_PUBLIC_URL (default: "http://ory-hydra-public.loopzero-system:4444")
	HydraPublicURL string

	// HydraAdminURL is the Ory Hydra admin API URL (client management, consent).
	// Env: HYDRA_ADMIN_URL (default: "http://ory-hydra-admin.loopzero-system:4445")
	HydraAdminURL string

	// HydraClientID is the OAuth2 client ID for the CLI (public client).
	// Env: HYDRA_CLIENT_ID (default: "loopzero-cli")
	HydraClientID string

	// EngineAPIURL is the internal URL of the LoopZero engine API (for proxy).
	// Env: ENGINE_API_URL (default: "http://loopzero-api.loopzero-system:8080")
	EngineAPIURL string

	// JWTIssuer is the expected issuer in JWT tokens (must match Hydra config).
	// Env: JWT_ISSUER (default: HydraPublicURL + "/")
	JWTIssuer string

	// Environment is the deployment environment (dev, staging, prod).
	// Env: LOOPZERO_ENV (default: "dev")
	Environment string

	// StripeAPIKey is the Stripe secret key for API calls.
	// Env: STRIPE_API_KEY (required in production)
	StripeAPIKey Secret

	// StripeWebhookSecret is the webhook signing secret for verifying Stripe events.
	// Env: STRIPE_WEBHOOK_SECRET (required in production)
	StripeWebhookSecret Secret

	// BillingReturnURL is the URL users return to after Stripe portal/checkout.
	// Env: BILLING_RETURN_URL (default: "http://localhost:3000")
	BillingReturnURL string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	cfg := Config{
		ListenAddr:      envOr("CLOUD_LISTEN_ADDR", ":13002"),
		KratosPublicURL: envOr("KRATOS_PUBLIC_URL", "http://ory-kratos-public.loopzero-system:80"),
		KratosAdminURL:  envOr("KRATOS_ADMIN_URL", "http://ory-kratos-admin.loopzero-system:80"),
		HydraPublicURL:  envOr("HYDRA_PUBLIC_URL", "http://ory-hydra-public.loopzero-system:4444"),
		HydraAdminURL:   envOr("HYDRA_ADMIN_URL", "http://ory-hydra-admin.loopzero-system:4445"),
		HydraClientID:   envOr("HYDRA_CLIENT_ID", "loopzero-cli"),
		EngineAPIURL:    envOr("ENGINE_API_URL", "http://loopzero-api.loopzero-system:8080"),
		Environment:     envOr("LOOPZERO_ENV", "dev"),
	}
	cfg.JWTIssuer = envOr("JWT_ISSUER", cfg.HydraPublicURL+"/")
	cfg.StripeAPIKey = Secret(envOr("STRIPE_API_KEY", ""))
	cfg.StripeWebhookSecret = Secret(envOr("STRIPE_WEBHOOK_SECRET", ""))
	cfg.BillingReturnURL = envOr("BILLING_RETURN_URL", "http://localhost:3000")
	return cfg
}

// Validate checks that required fields are set for production.
func (c Config) Validate() error {
	if c.Environment == "prod" {
		if c.HydraPublicURL == "" {
			return fmt.Errorf("HYDRA_PUBLIC_URL is required in production")
		}
		if c.KratosPublicURL == "" {
			return fmt.Errorf("KRATOS_PUBLIC_URL is required in production")
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
