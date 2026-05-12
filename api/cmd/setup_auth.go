// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"os"

	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

// AuthDeps holds authentication and authorization components.
type AuthDeps struct {
	TokenValidator middleware.TokenValidator
}

// WireAuthFailureAuditor wires the package-level DefaultAuthAuditor used by
// auth middleware failure paths (Phase A.4 of MEET-2026-05-12 T1.3).
//
// Must be called AFTER setupDatabase. Nil-safe — if dbAdapter is nil
// (test/dev mode), the auditor is also nil and failure paths skip
// emission silently.
//
// EC-NEW-12: emission failures are fail-open — never block auth response.
func WireAuthFailureAuditor(dbAdapter *db.PgxAdapter, logger *slog.Logger) {
	if dbAdapter == nil {
		logger.Warn("auth failure auditor: dbAdapter nil; failure emissions disabled")
		return
	}
	writer := func(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error {
		return db.WriteAuditLog(ctx, dbAdapter, db.AuditEntry{
			TenantID:      tenantID,
			Action:        action,
			Actor:         actor,
			ResourceType:  resourceType,
			ResourceID:    resourceID,
			CorrelationID: correlationID,
			Metadata:      metadata,
		})
	}
	middleware.DefaultAuthAuditor = middleware.NewAuthFailureAuditor(writer, logger)
	logger.Info("auth failure auditor wired", "component", "audit_log")
}

// setupAuth configures JWT validation (RSA-only) and service-to-service auth secrets.
// MEET-2026-04-07-039: B3+B4 — engine uses RSA-only with strict audience.
func setupAuth(logger *slog.Logger) *AuthDeps {
	// JWT-only auth — uses pkg/jwt (single source of truth).
	jwksURL := os.Getenv("JWKS_URL")
	jwtIssuer := os.Getenv("JWT_ISSUER")
	rsaValidator := loopzerojwt.NewValidator(jwksURL, jwtIssuer, "loopzero-engine")

	// B3: Engine drops HMAC entirely — RSA-only validation.
	tokenValidator := middleware.NewJWTOnlyValidator(rsaValidator, nil)
	logger.Info("JWT auth enabled (RSA-only)", "jwks_url", jwksURL)

	// Service-to-service auth for internal proxy calls (cloud->engine).
	// S1.2: Dual-key rotation — accepts current OR previous during rotation window.
	middleware.ServiceAuthSecret = os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET")
	middleware.ServiceAuthSecretPrevious = os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS")

	// MEET-052: Platform admin API key for cross-tenant operations.
	// Dual-key rotation: accepts current OR previous during rotation window.
	middleware.AdminAPIKey = os.Getenv("LOOPZERO_ADMIN_API_KEY")
	middleware.AdminAPIKeyPrevious = os.Getenv("LOOPZERO_ADMIN_API_KEY_PREVIOUS")

	return &AuthDeps{
		TokenValidator: tokenValidator,
	}
}
