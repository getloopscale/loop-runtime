// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package apitest exports API server types for integration and E2E testing.
//
// This package provides type aliases and a router factory so that external
// test modules (e.g., infra/tests/e2e) can create an in-process API server
// with test doubles, without importing api/internal/* packages directly.
package apitest

import (
	"log/slog"
	"net/http"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
)

// --- Type aliases for DIP interfaces (test doubles implement these) ---

// TokenValidator validates authentication tokens.
type TokenValidator = middleware.TokenValidator

// TokenValidationResult holds the result of validating a token.
type TokenValidationResult = middleware.TokenValidationResult

// ErrInvalidToken is returned when token validation fails.
var ErrInvalidToken = middleware.ErrInvalidToken

// MembershipStore retrieves tenant memberships for a user.
type MembershipStore = middleware.MembershipStore

// TenantMembership represents a user's membership in a tenant.
type TenantMembership = middleware.TenantMembership

// ProjectStore abstracts project persistence.
type ProjectStore = routes.ProjectStore

// CreateProjectInput is the input for creating a project.
type CreateProjectInput = routes.CreateProjectInput

// ProjectResult is the result of creating a project.
type ProjectResult = routes.ProjectResult

// ErrProjectExists is returned when a project already exists.
var ErrProjectExists = routes.ErrProjectExists

// TenantCRManager abstracts Kubernetes Tenant CR operations.
type TenantCRManager = routes.TenantCRManager

// TenantCRSpec holds the spec for creating/updating a Tenant CR.
type TenantCRSpec = routes.TenantCRSpec

// IdempotencyStore abstracts idempotency key storage.
type IdempotencyStore = middleware.IdempotencyStore

// IdempotencyRecord holds a cached idempotency response.
type IdempotencyRecord = middleware.IdempotencyRecord

// --- Router factory ---

// Config mirrors server.Config for test use.
type Config struct {
	Port             string
	Logger           *slog.Logger
	DB               routes.DBHealthChecker
	TokenValidator   TokenValidator
	MemberStore      MembershipStore
	ProjectStore     ProjectStore
	TenantCRMgr      TenantCRManager
	IdempotencyStore IdempotencyStore
}

// NewRouter creates a chi router configured with the given test config.
// Use httptest.NewServer(NewRouter(cfg)) to start an in-process API server.
func NewRouter(cfg Config) http.Handler {
	return server.NewRouter(server.Config{
		Port:             cfg.Port,
		Logger:           cfg.Logger,
		DB:               cfg.DB,
		TokenValidator:   cfg.TokenValidator,
		MemberStore:      cfg.MemberStore,
		ProjectStore:     cfg.ProjectStore,
		TenantCRMgr:      cfg.TenantCRMgr,
		IdempotencyStore: cfg.IdempotencyStore,
	})
}
