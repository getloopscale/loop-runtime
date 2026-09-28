// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Wiring: the route exists on the REAL router, not only the handler in isolation.
//
// The defect this prevents is the classic "implemented but not wired": the
// handler tests in internal/routes would pass identically with the route never
// mounted, and the /status panel would show the engine as `unreachable` —
// indistinguishable from a service that is down. A handler with no caller is
// not delivered.
func TestRouter_MountsFleetVersionRoute(t *testing.T) {
	r := NewRouter(Config{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/version = %d, want 200 (is the route mounted?)", rec.Code)
	}
	var got struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version == "" || got.BuiltAt == "" {
		t.Errorf("incomplete payload through the router: version=%q built_at=%q", got.Version, got.BuiltAt)
	}
}

// The route is PUBLIC: the collector probes without a session. Behind the JWT
// check, the panel would read the 401 as a service that is unavailable.
func TestRouter_FleetVersionNeedsNoAuth(t *testing.T) {
	r := NewRouter(Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil) // no Authorization
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("GET /v1/version required auth (%d) — the collector probes without a session", rec.Code)
	}
}
