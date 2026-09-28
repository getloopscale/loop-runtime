// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Roles middleware tests ---

func TestRequireRole_AdminRequired_OwnerPasses(t *testing.T) {
	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Errorf("owner should pass admin check, got %d", rec.Code)
	}
}

func TestRequireRole_AdminRequired_MemberFails(t *testing.T) {
	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "member",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusForbidden {
		t.Errorf("member should fail admin check, got %d", rec.Code)
	}
}

func TestRequireRole_NoUserContext_Returns401(t *testing.T) {
	handler := RequireRole("member")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without user context, got %d", rec.Code)
	}
}

func TestHasMinRole(t *testing.T) {
	tests := []struct {
		user, min string
		want      bool
	}{
		{"owner", "owner", true},
		{"owner", "admin", true},
		{"owner", "member", true},
		{"admin", "owner", false},
		{"admin", "admin", true},
		{"admin", "member", true},
		{"member", "admin", false},
		{"member", "member", true},
	}
	for _, tt := range tests {
		got := HasMinRole(tt.user, tt.min)
		if got != tt.want {
			t.Errorf("HasMinRole(%q, %q) = %v, want %v", tt.user, tt.min, got, tt.want)
		}
	}
}

func TestIsKnownRole(t *testing.T) {
	for role, want := range map[string]bool{"owner": true, "admin": true, "member": true, "": false, "superuser": false} {
		if got := IsKnownRole(role); got != want {
			t.Errorf("IsKnownRole(%q) = %v, want %v", role, got, want)
		}
	}
}
