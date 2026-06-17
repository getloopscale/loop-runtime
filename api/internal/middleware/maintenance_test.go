// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/middleware"
)

func newMaintenanceHandler() http.Handler {
	return middleware.MaintenanceMode(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestMaintenanceMode_PassthroughWhenUnset(t *testing.T) {
	t.Setenv("LOOPZERO_MAINTENANCE_MODE", "")

	handler := newMaintenanceHandler()

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/v1/deploy", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200 when LOOPZERO_MAINTENANCE_MODE is unset, got %d", method, rec.Code)
		}
	}
}

func TestMaintenanceMode_BlocksMutationsWhenSet(t *testing.T) {
	t.Setenv("LOOPZERO_MAINTENANCE_MODE", "1")

	handler := newMaintenanceHandler()

	mutatingMethods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, method := range mutatingMethods {
		req := httptest.NewRequest(method, "/api/v1/deploy", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503 in maintenance mode, got %d", method, rec.Code)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("%s: failed to decode response body: %v", method, err)
		}
		if body["code"] != "MAINTENANCE_MODE" {
			t.Errorf("%s: expected code=MAINTENANCE_MODE, got %v", method, body["code"])
		}
		if body["retry_after_seconds"] != float64(300) {
			t.Errorf("%s: expected retry_after_seconds=300, got %v", method, body["retry_after_seconds"])
		}
	}
}

func TestMaintenanceMode_AllowsReadsWhenSet(t *testing.T) {
	t.Setenv("LOOPZERO_MAINTENANCE_MODE", "true")

	handler := newMaintenanceHandler()

	readMethods := []string{http.MethodGet, http.MethodHead, http.MethodOptions}
	for _, method := range readMethods {
		req := httptest.NewRequest(method, "/api/v1/projects", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200 for read-only method in maintenance mode, got %d", method, rec.Code)
		}
	}
}

func TestMaintenanceMode_AllowsHealthEndpoint(t *testing.T) {
	t.Setenv("LOOPZERO_MAINTENANCE_MODE", "1")

	handler := newMaintenanceHandler()

	// POST to health endpoint should pass through even in maintenance mode.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for health endpoint POST in maintenance mode, got %d", rec.Code)
	}
}

func TestMaintenanceMode_AllowsInternalRoutes(t *testing.T) {
	t.Setenv("LOOPZERO_MAINTENANCE_MODE", "1")

	handler := newMaintenanceHandler()

	// Internal routes are called by platform components (ArgoCD, controllers),
	// not by users. They must pass through during maintenance to prevent
	// in-flight deploys from deadlocking.
	internalPaths := []string{
		"/internal/argocd-callback",
		"/internal/bootstrap",
	}
	for _, path := range internalPaths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("POST %s: expected 200 for internal route in maintenance mode, got %d", path, rec.Code)
		}
	}
}

func TestMaintenanceMode_CustomMessage(t *testing.T) {
	customMsg := "PG resize in progress, back in 5 minutes"
	t.Setenv("LOOPZERO_MAINTENANCE_MODE", customMsg)

	handler := newMaintenanceHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["message"] != customMsg {
		t.Errorf("expected custom message %q, got %q", customMsg, body["message"])
	}
}

func TestMaintenanceMode_RetryAfterHeader(t *testing.T) {
	t.Setenv("LOOPZERO_MAINTENANCE_MODE", "1")

	handler := newMaintenanceHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter != "300" {
		t.Errorf("expected Retry-After=300, got %q", retryAfter)
	}
}
