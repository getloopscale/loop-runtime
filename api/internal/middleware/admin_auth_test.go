// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateAdminAPIKey_CurrentKey(t *testing.T) {
	AdminAPIKey = "test-admin-key-current"
	AdminAPIKeyPrevious = ""
	defer func() { AdminAPIKey = ""; AdminAPIKeyPrevious = "" }()

	if !validateAdminAPIKey("test-admin-key-current") {
		t.Error("should accept current key")
	}
}

func TestValidateAdminAPIKey_PreviousKey(t *testing.T) {
	AdminAPIKey = "test-admin-key-current"
	AdminAPIKeyPrevious = "test-admin-key-previous"
	defer func() { AdminAPIKey = ""; AdminAPIKeyPrevious = "" }()

	if !validateAdminAPIKey("test-admin-key-previous") {
		t.Error("should accept previous key during rotation")
	}
}

func TestValidateAdminAPIKey_InvalidKey(t *testing.T) {
	AdminAPIKey = "test-admin-key-current"
	AdminAPIKeyPrevious = "test-admin-key-previous"
	defer func() { AdminAPIKey = ""; AdminAPIKeyPrevious = "" }()

	if validateAdminAPIKey("wrong-key") {
		t.Error("should reject invalid key")
	}
}

func TestValidateAdminAPIKey_EmptyConfig(t *testing.T) {
	AdminAPIKey = ""
	AdminAPIKeyPrevious = ""

	if validateAdminAPIKey("any-key") {
		t.Error("should reject when admin key is not configured")
	}
}

func TestRequireAdmin_MissingHeader(t *testing.T) {
	AdminAPIKey = "test-key"
	defer func() { AdminAPIKey = "" }()

	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("DELETE", "/api/v1/tenants/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("missing header should return 403, got %d", rec.Code)
	}
}

func TestRequireAdmin_InvalidKey(t *testing.T) {
	AdminAPIKey = "test-key"
	defer func() { AdminAPIKey = "" }()

	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("DELETE", "/api/v1/tenants/test", nil)
	req.Header.Set("X-Admin-Key", "wrong-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("invalid key should return 403, got %d", rec.Code)
	}
}

func TestRequireAdmin_ValidKey(t *testing.T) {
	AdminAPIKey = "test-key"
	defer func() { AdminAPIKey = "" }()

	called := false
	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("DELETE", "/api/v1/tenants/test", nil)
	req.Header.Set("X-Admin-Key", "test-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("valid key should return 200, got %d", rec.Code)
	}
	if !called {
		t.Error("handler should have been called with valid key")
	}
}

func TestRequireAdmin_DualKeyRotation(t *testing.T) {
	AdminAPIKey = "new-key"
	AdminAPIKeyPrevious = "old-key"
	defer func() { AdminAPIKey = ""; AdminAPIKeyPrevious = "" }()

	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Old key should still work during rotation
	req := httptest.NewRequest("DELETE", "/api/v1/tenants/test", nil)
	req.Header.Set("X-Admin-Key", "old-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("previous key should be accepted during rotation, got %d", rec.Code)
	}
}
