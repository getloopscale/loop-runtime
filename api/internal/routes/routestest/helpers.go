// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routestest holds the request-shaping helpers every route family's
// tests need — injecting a UserContext, setting a chi URL param, building the
// two standard user contexts.
//
// # Why it is a package and not a _test.go file
//
// These four lived in `routes/test_helpers_test.go` and were used by THIRTY test
// files. A helper in a `_test.go` is compiled only into its own package's test
// binary, so the moment a family moves into its own subpackage its tests lose
// access to it — measured the instant the `tenants` family moved and its test
// stopped compiling on `undefined: withAuth`.
//
// The first family to move (`version`) never hit this: it has no auth. Family two
// did, which is the argument for moving one family at a time — the requirement
// for the remaining fourteen surfaced from a real move rather than from guessing.
//
// It is the test-side sibling of `httpio`: a leaf every family may import, and
// that imports no family. `routes-acyclic` registers it with an empty allowed
// set for exactly that reason.
package routestest

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/middleware"
)

// WithAuth injects a UserContext into the request context for tests.
func WithAuth(r *http.Request, uc *middleware.UserContext) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.UserContextKeyPublic(), uc)
	return r.WithContext(ctx)
}

// WithChiParam sets chi URL parameters on the request for tests.
func WithChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	return r.WithContext(ctx)
}

// DefaultUserContext returns a standard authenticated user for tests.
func DefaultUserContext() *middleware.UserContext {
	return &middleware.UserContext{
		UserID:   "github-testuser",
		Email:    "test@example.com",
		TenantID: "tn-test-123",
		Role:     "owner",
	}
}

// MemberUserContext returns a non-admin user for permission tests.
func MemberUserContext() *middleware.UserContext {
	return &middleware.UserContext{
		UserID:   "github-memberuser",
		Email:    "member@example.com",
		TenantID: "tn-test-123",
		Role:     "member",
	}
}
