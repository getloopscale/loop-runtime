// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// Shared fixtures for tests that drive the REAL router: a token validator and
// a membership store of one method each are all the authenticated group needs
// to mount, so chi.Walk sees every protected route instead of zero.

type fixedTokenValidator struct{}

func (fixedTokenValidator) ValidateToken(context.Context, string) (*auth.TokenValidationResult, error) {
	return &auth.TokenValidationResult{UserID: "u-fixed", Email: "u@example.test"}, nil
}

// fixedMembership makes the fixture user the owner of the fixture tenant, so
// guard tests reach past the role gates to the guard they are about.
type fixedMembership struct{}

func (fixedMembership) GetMemberships(context.Context, string) ([]auth.TenantMembership, error) {
	return []auth.TenantMembership{{TenantID: "tn-fixed", Role: "owner"}}, nil
}

// withFixedUser attaches an authenticated user to a request that bypasses the
// router's auth middleware.
func withFixedUser(req *http.Request) *http.Request {
	uc := &auth.UserContext{UserID: "u-fixed", Email: "u@example.test", TenantID: "tn-fixed"}
	return req.WithContext(context.WithValue(req.Context(), auth.UserContextKeyPublic(), uc))
}

type routeRef struct{ method, pattern string }

// mutatingRoutes walks the real router; no hand-kept list can go stale.
func mutatingRoutes(t *testing.T, r chi.Router) []routeRef {
	t.Helper()
	var out []routeRef
	err := chi.Walk(r, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			out = append(out, routeRef{method: method, pattern: pattern})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	return out
}

// concretePath replaces every `{param}` with a fixed value.
func concretePath(pattern string) string {
	var b strings.Builder
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "" {
			continue
		}
		b.WriteString("/")
		if strings.HasPrefix(seg, "{") {
			b.WriteString("fixed")
		} else {
			b.WriteString(strings.TrimSuffix(seg, "/*"))
		}
	}
	if b.Len() == 0 {
		return "/"
	}
	return b.String()
}
