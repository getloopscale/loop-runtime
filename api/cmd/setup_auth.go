// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/clientip"
	"log/slog"
	"os"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	loopruntimejwt "github.com/getloopscale/contracts/jwt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// AuthDeps holds authentication and authorization components.
type AuthDeps struct {
	TokenValidator auth.TokenValidator
	// Config is the authentication configuration read from the environment
	// once, here. main completes it with the failure auditor (which needs the
	// database) and builds the Authenticator the router uses.
	Config auth.Config
}

// buildAuthFailureAuditor returns the auditor that persists authentication
// rejections to audit_log (Phase A.4 of MEET-2026-05-12 T1.3), or nil when
// there is no database — the Authenticator then records nothing.
//
// EC-NEW-12: emission failures are fail-open — never block the auth response.
func buildAuthFailureAuditor(dbAdapter *db.PgxAdapter, logger *slog.Logger, clientIP clientip.Resolver) *auth.AuthFailureAuditor {
	if dbAdapter == nil {
		logger.Warn("auth failure auditor: dbAdapter nil; failure emissions disabled")
		return nil
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
	logger.Info("auth failure auditor wired", "component", "audit_log")
	return auth.NewAuthFailureAuditor(writer, logger, clientIP)
}

// setupAuth configures JWT validation (RSA-only) and service-to-service auth secrets.
// MEET-2026-04-07-039: B3+B4 — engine uses RSA-only with strict audience.
//
// CANONICAL FIX 2026-06-15 (multi-audience per RFC 7519 § 4.1.3): loop-runtime-api
// MUST accept BOTH "loop-runtime-engine" (CLI + bootstrap JWTs minted directly by
// Hydra for engine-direct access) AND "loop-console" (HMAC session JWTs
// minted by loop-console's /v1/auth/session/token endpoint for browser flows
// proxied via /engine/* paths). Single-audience validation forced
// bifurcated test patterns (different JWT shapes per CLOUD_URL target);
// multi-audience accepts EITHER shape. Mirrors the canonical pattern
// adopted by AWS Cognito, Auth0, Okta, Google Cloud Identity.
func setupAuth(logger *slog.Logger) *AuthDeps {
	// JWT-only auth — uses pkg/jwt (single source of truth).
	jwksURL := os.Getenv("JWKS_URL")
	jwtIssuer := os.Getenv("JWT_ISSUER")
	rsaValidator := loopruntimejwt.NewValidatorWithAudiences(jwksURL, jwtIssuer,
		[]string{"loop-runtime-engine", "loop-console"})

	// B3: Engine drops HMAC entirely — RSA-only validation.
	tokenValidator := auth.NewJWTOnlyValidator(rsaValidator, nil)
	logger.Info("JWT auth enabled (RSA-only)", "jwks_url", jwksURL)

	// A configured but unreachable replay-protection store fails the boot
	// rather than silently dropping cross-replica replay protection.
	nonceStore, err := setupServiceAuthNonceStore(logger)
	if err != nil {
		logger.Error("service-auth nonce store unavailable", "error", err)
		os.Exit(1)
	}

	// Service-to-service auth for internal proxy calls (cloud->engine), the
	// operator (admin) service secret, and the platform-admin API key — each
	// with a _PREVIOUS sibling for zero-downtime rotation (S1.2, MEET-052).
	// Ed25519 dual-control (ADR 2026-05-13-service-auth-ed25519-adoption): a
	// malformed key CRASHES startup rather than leaving the trust set empty
	// (RFC Q5 fail-closed). The replay-protection nonce store is Redis-backed
	// when LOOP_RUNTIME_SERVICE_AUTH_NONCE_REDIS_ADDR is set (ADR Q2).
	cfg := auth.Config{
		ServiceSecret:              os.Getenv("LOOP_RUNTIME_SERVICE_AUTH_SECRET"),
		ServiceSecretPrevious:      os.Getenv("LOOP_RUNTIME_SERVICE_AUTH_SECRET_PREVIOUS"),
		AdminServiceSecret:         os.Getenv("LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET"),
		AdminServiceSecretPrevious: os.Getenv("LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET_PREVIOUS"),
		AdminAPIKey:                os.Getenv("LOOP_RUNTIME_ADMIN_API_KEY"),
		AdminAPIKeyPrevious:        os.Getenv("LOOP_RUNTIME_ADMIN_API_KEY_PREVIOUS"),
		Ed25519PublicKeys:          loadServiceAuthEd25519Trust(logger),
		RequireSignature:           serviceAuthRequireSignature(),
		NonceStore:                 nonceStore,
	}

	return &AuthDeps{
		TokenValidator: tokenValidator,
		Config:         cfg,
	}
}

// loadServiceAuthEd25519Trust reads the public-key env vars and returns the
// Ed25519 trust set. Exits via
// logger.Error + os.Exit(1) on parse failure per Q5 fail-closed.
//
// Split from the error-returning variant for caller ergonomics: tests
// drive loadServiceAuthEd25519TrustE() and assert error shape; the
// production entry-point loadServiceAuthEd25519Trust() converts the
// error to a fatal log so an invalid key configuration is impossible
// to miss.
func loadServiceAuthEd25519Trust(logger *slog.Logger) []ed25519.PublicKey {
	trust, err := loadServiceAuthEd25519TrustE()
	if err != nil {
		logger.Error("service-auth Ed25519 trust-set load failed (fail-closed)", "error", err)
		os.Exit(1)
	}
	if n := len(trust); n > 0 {
		logger.Info("service-auth Ed25519 dual-control enabled", "trusted_keys", n)
	} else {
		logger.Warn("service-auth Ed25519 trust set is empty — dual-control inert; signed requests will fail-closed (set LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT to enable)")
	}
	return trust
}

// loadServiceAuthEd25519TrustE is the error-returning loader. Returns an
// empty trust set when both env vars are absent
// (Phase 1 default before operator bootstrap). Returns an error when
// either env var is present but malformed.
func loadServiceAuthEd25519TrustE() ([]ed25519.PublicKey, error) {
	current := os.Getenv("LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT")
	previous := os.Getenv("LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS")

	trust := []ed25519.PublicKey{}
	if current != "" {
		pub, err := decodeEd25519PublicKey(current)
		if err != nil {
			return nil, fmt.Errorf("LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT: %w", err)
		}
		trust = append(trust, pub)
	}
	if previous != "" {
		pub, err := decodeEd25519PublicKey(previous)
		if err != nil {
			return nil, fmt.Errorf("LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS: %w", err)
		}
		trust = append(trust, pub)
	}
	return trust, nil
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

// serviceAuthRequireSignature reads LOOP_RUNTIME_SERVICE_AUTH_REQUIRE_SIGNATURE, the
// middleware's Phase 2 gate. Accepts "true", "1", "yes" (case-insensitive). Any other value
// — including empty — keeps the default false (Phase 1 accept-both).
//
// Per ADR Q3: the operator flips this only after the 7-day soak at
// 100% present rate. Setting it true on a stack without a deployed
// signer would block all cloud→engine traffic.
func serviceAuthRequireSignature() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOOP_RUNTIME_SERVICE_AUTH_REQUIRE_SIGNATURE"))) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
