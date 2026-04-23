// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

// withAuth injects a UserContext into the request context for tests.
func withAuth(r *http.Request, uc *middleware.UserContext) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.UserContextKeyPublic(), uc)
	return r.WithContext(ctx)
}

// withChiParam sets chi URL parameters on the request for tests.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	return r.WithContext(ctx)
}

// withChiParams sets multiple chi URL parameters on the request for tests.
func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	return r.WithContext(ctx)
}

// defaultUserContext returns a standard authenticated user for tests.
func defaultUserContext() *middleware.UserContext {
	return &middleware.UserContext{
		UserID:   "github-testuser",
		Email:    "test@example.com",
		TenantID: "tn-test-123",
		Role:     "owner",
	}
}

// memberUserContext returns a non-admin user for permission tests.
func memberUserContext() *middleware.UserContext {
	return &middleware.UserContext{
		UserID:   "github-memberuser",
		Email:    "member@example.com",
		TenantID: "tn-test-123",
		Role:     "member",
	}
}
