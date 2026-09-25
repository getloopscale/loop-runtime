// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"github.com/getloopscale/loopzero/api/internal/middleware/clientip"
	"log/slog"
	"os"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	loopzerojwt "github.com/getloopscale/loopzero-contracts/jwt"

	"github.com/getloopscale/loopzero/api/internal/db"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// AuthDeps holds authentication and authorization components.
type AuthDeps struct {
	TokenValidator auth.TokenValidator
}

// WireAuthFailureAuditor wires the package-level DefaultAuthAuditor used by
// auth middleware failure paths (Phase A.4 of MEET-2026-05-12 T1.3).
//
// Must be called AFTER setupDatabase. Nil-safe — if dbAdapter is nil
// (test/dev mode), the auditor is also nil and failure paths skip
// emission silently.
//
// EC-NEW-12: emission failures are fail-open — never block auth response.
func WireAuthFailureAuditor(dbAdapter *db.PgxAdapter, logger *slog.Logger, clientIP clientip.Resolver) {
	if dbAdapter == nil {
		logger.Warn("auth failure auditor: dbAdapter nil; failure emissions disabled")
		return
	}
	writer := func(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error {
		return store.WriteAuditLog(ctx, dbAdapter, store.AuditEntry{
			TenantID:      tenantID,
			Action:        action,
			Actor:         actor,
			ResourceType:  resourceType,
			ResourceID:    resourceID,
			CorrelationID: correlationID,
			Metadata:      metadata,
		})
	}
	auth.DefaultAuthAuditor = auth.NewAuthFailureAuditor(writer, logger, clientIP)
	logger.Info("auth failure auditor wired", "component", "audit_log")
}

// setupAuth configures JWT validation (RSA-only) and service-to-service auth secrets.
// MEET-2026-04-07-039: B3+B4 — engine uses RSA-only with strict audience.
//
// CANONICAL FIX 2026-06-15 (multi-audience per RFC 7519 § 4.1.3): loopzero-api
// MUST accept BOTH "loopzero-engine" (CLI + bootstrap JWTs minted directly by
// Hydra for engine-direct access) AND "loopzero-cloud" (HMAC session JWTs
// minted by loopzero-cloud's /v1/auth/session/token endpoint for browser flows
// proxied via /engine/* paths). Single-audience validation forced
// bifurcated test patterns (different JWT shapes per CLOUD_URL target);
// multi-audience accepts EITHER shape. Mirrors the canonical pattern
// adopted by AWS Cognito, Auth0, Okta, Google Cloud Identity.
func setupAuth(logger *slog.Logger) *AuthDeps {
	// JWT-only auth — uses pkg/jwt (single source of truth).
	jwksURL := os.Getenv("JWKS_URL")
	jwtIssuer := os.Getenv("JWT_ISSUER")
	rsaValidator := loopzerojwt.NewValidatorWithAudiences(jwksURL, jwtIssuer,
		[]string{"loopzero-engine", "loopzero-cloud"})

	// B3: Engine drops HMAC entirely — RSA-only validation.
	tokenValidator := auth.NewJWTOnlyValidator(rsaValidator, nil)
	logger.Info("JWT auth enabled (RSA-only)", "jwks_url", jwksURL)

	// Service-to-service auth for internal proxy calls (cloud->engine).
	// S1.2: Dual-key rotation — accepts current OR previous during rotation window.
	auth.ServiceAuthSecret = os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET")
	auth.ServiceAuthSecretPrevious = os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS")

	// MEET-052: Platform admin API key for cross-tenant operations.
	// Dual-key rotation: accepts current OR previous during rotation window.
	auth.AdminAPIKey = os.Getenv("LOOPZERO_ADMIN_API_KEY")
	auth.AdminAPIKeyPrevious = os.Getenv("LOOPZERO_ADMIN_API_KEY_PREVIOUS")

	// Ed25519 dual-control (ADR 2026-05-13-service-auth-ed25519-adoption).
	// Reads LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_CURRENT (+ optional ...PREVIOUS
	// for the 7-day rotation overlap) and populates the middleware trust
	// set. Per RFC Q5 fail-closed: malformed keys CRASH startup rather
	// than silently leave the trust set empty.
	loadServiceAuthEd25519Trust(logger)
	loadServiceAuthRequireSignatureFlag()

	// Replay-protection nonce store (ADR Q2). In-memory by default;
	// switches to Redis-backed when LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR
	// is set. Multi-replica API needs Redis for cross-replica replay
	// protection; single-replica dev/test uses in-memory fallback.
	setupServiceAuthNonceStore(logger)

	return &AuthDeps{
		TokenValidator: tokenValidator,
	}
}

// loadServiceAuthEd25519Trust reads the public-key env vars and
// populates auth.ServiceAuthEd25519PublicKeys. Panics via
// logger.Error + os.Exit(1) on parse failure per Q5 fail-closed.
//
// Split from the error-returning variant for caller ergonomics: tests
// drive loadServiceAuthEd25519TrustE() and assert error shape; the
// production entry-point loadServiceAuthEd25519Trust() converts the
// error to a fatal log so an invalid key configuration is impossible
// to miss.
func loadServiceAuthEd25519Trust(logger *slog.Logger) {
	if err := loadServiceAuthEd25519TrustE(); err != nil {
		logger.Error("service-auth Ed25519 trust-set load failed (fail-closed)", "error", err)
		os.Exit(1)
	}
	if n := len(auth.ServiceAuthEd25519PublicKeys); n > 0 {
		logger.Info("service-auth Ed25519 dual-control enabled", "trusted_keys", n)
	} else {
		logger.Warn("service-auth Ed25519 trust set is empty — dual-control inert; signed requests will fail-closed (set LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_CURRENT to enable)")
	}
}

// loadServiceAuthEd25519TrustE is the error-returning loader. Returns
// nil and leaves the trust set empty when both env vars are absent
// (Phase 1 default before operator bootstrap). Returns an error when
// either env var is present but malformed.
func loadServiceAuthEd25519TrustE() error {
	current := os.Getenv("LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_CURRENT")
	previous := os.Getenv("LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS")

	trust := []ed25519.PublicKey{}
	if current != "" {
		pub, err := decodeEd25519PublicKey(current)
		if err != nil {
			return fmt.Errorf("LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_CURRENT: %w", err)
		}
		trust = append(trust, pub)
	}
	if previous != "" {
		pub, err := decodeEd25519PublicKey(previous)
		if err != nil {
			return fmt.Errorf("LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS: %w", err)
		}
		trust = append(trust, pub)
	}
	auth.ServiceAuthEd25519PublicKeys = trust
	return nil
}

// decodeEd25519PublicKey decodes a base64-encoded raw Ed25519 public
// key. Returns an error on bad base64 OR wrong key length (the only
// way an Ed25519 public key has the right shape is if it's exactly
// ed25519.PublicKeySize bytes).
func decodeEd25519PublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("not valid base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("wrong key length %d (want %d)", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// loadServiceAuthRequireSignatureFlag reads
// LOOPZERO_SERVICE_AUTH_REQUIRE_SIGNATURE and flips the middleware Phase 2
// gate. Accepts "true", "1", "yes" (case-insensitive). Any other value
// — including empty — keeps the default false (Phase 1 accept-both).
//
// Per ADR Q3: the operator flips this only after the 7-day soak at
// 100% present rate. Setting it true on a stack without a deployed
// signer would block all cloud→engine traffic.
func loadServiceAuthRequireSignatureFlag() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOOPZERO_SERVICE_AUTH_REQUIRE_SIGNATURE"))) {
	case "true", "1", "yes":
		auth.ServiceAuthRequireSignature = true
	default:
		auth.ServiceAuthRequireSignature = false
	}
}
