// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubResolver struct {
	role string
	err  error
}

func (s stubResolver) GetRole(_ context.Context, _ string) (string, error) {
	return s.role, s.err
}

func requestWithUser(u *UserContext) *http.Request {
	req := httptest.NewRequest("POST", "/v1/billing/checkout", nil)
	if u != nil {
		req = req.WithContext(context.WithValue(req.Context(), userCtxKey, u))
	}
	return req
}

// T1.3 — a member must be blocked from an owner-only billing route.
func TestRequireRole_BlocksMemberOnOwnerRoute(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := RequireRole(stubResolver{role: "member"}, "owner")

	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, requestWithUser(&UserContext{Subject: "u1"}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for member, got %d", rec.Code)
	}
	if called {
		t.Fatal("next handler must not run for insufficient role")
	}
}

// T1.3 — an owner reaches the protected handler, with role stamped in context.
func TestRequireRole_AllowsOwner(t *testing.T) {
	var seenRole string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenRole = GetUser(r.Context()).Role
	})
	mw := RequireRole(stubResolver{role: "owner"}, "owner")

	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, requestWithUser(&UserContext{Subject: "u1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected owner to pass, got %d", rec.Code)
	}
	if seenRole != "owner" {
		t.Fatalf("expected resolved role stamped in context, got %q", seenRole)
	}
}

// T1.3 — unauthenticated requests are rejected before resolving a role.
func TestRequireRole_Unauthenticated(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not run when unauthenticated")
	})
	mw := RequireRole(stubResolver{role: "owner"}, "owner")

	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, requestWithUser(nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T1.3 — a resolver failure must fail closed (deny), not fall through.
func TestRequireRole_ResolverErrorFailsClosed(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := RequireRole(stubResolver{err: errors.New("db down")}, "owner")

	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, requestWithUser(&UserContext{Subject: "u1"}))

	if rec.Code < 400 {
		t.Fatalf("expected error status on resolver failure, got %d", rec.Code)
	}
	if called {
		t.Fatal("next handler must not run when role resolution fails")
	}
}
