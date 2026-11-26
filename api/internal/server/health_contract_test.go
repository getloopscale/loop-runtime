// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The ecosystem-canonical readiness name (B-123, contract rule D5).
//
// # What was here
//
// `GET /api/v1/health` for liveness and `GET /api/v1/health/deep` for readiness. The behaviour is
// right — `deep` checks the database and every registered service, answering 503 when any is
// degraded — but the NAME says how the check is implemented rather than what it answers. A
// consumer reading the surface has no way to know that "deep" is the question an orchestrator
// asks before sending traffic.
//
// Measured across nine services for the HTTP surface contract (B-115): nine different combinations
// of health paths, and this repo has the most of any of them. `/api/v1/health/ready` is the
// spelling the rest of the ecosystem is converging on.
//
// # Why adding rather than renaming
//
// `/api/v1/health/deep` has callers — the maintenance middleware special-cases the health prefix,
// and deployment probes point at it. Renaming takes the service out of rotation the moment this
// deploys. Additive by contract: both answer the same handler, and removal is its own item after a
// deprecation deadline.
//
// # Why it shares the handler rather than getting its own
//
// A second implementation of "check the database and every registered service" would drift, and
// the half that would drift silently is the degraded-detection — the part that keeps traffic off a
// node that cannot serve.

// The router is built the same way `fleet_version_route_test.go` builds it: from the REAL
// `NewRouter`, not from an isolated handler. A handler test would pass identically with the route
// never mounted — "implemented but not wired" is the defect this package already learned to guard
// against, and a health route that exists and is unreachable is the same defect with worse
// consequences.
func TestHealthContract_CanonicalReadinessIsServed(t *testing.T) {
	r := NewRouter(Config{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil))

	if rec.Code == http.StatusNotFound {
		t.Fatalf("GET /api/v1/health/ready = 404; it is the contract's readiness name and was not served")
	}
}

func TestHealthContract_CanonicalReadinessAnswersTheSameAsDeep(t *testing.T) {
	// Same handler, not a second implementation. A copy of "check the database and every
	// registered service" would drift, and the half that drifts quietly is degraded-detection —
	// which is what keeps traffic off a node that cannot serve.
	r := NewRouter(Config{})

	deep := httptest.NewRecorder()
	r.ServeHTTP(deep, httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil))

	ready := httptest.NewRecorder()
	r.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil))

	if deep.Code != ready.Code {
		t.Errorf("deep = %d, ready = %d; the two names must answer identically", deep.Code, ready.Code)
	}
	if deep.Body.String() != ready.Body.String() {
		t.Errorf("the two names returned different bodies:\n deep:  %s\n ready: %s", deep.Body.String(), ready.Body.String())
	}
}

func TestHealthContract_LivenessStaysCheap(t *testing.T) {
	// Liveness must not inherit the dependency checks. A database blip failing liveness gets a
	// recoverable process restarted, which loses in-flight work and does not fix the database.
	r := NewRouter(Config{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/health = %d, want 200 unconditionally", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("liveness body is not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("liveness status = %q, want ok", body["status"])
	}
}

func TestHealthContract_TheDeepNameStillAnswers(t *testing.T) {
	// It has callers: deployment probes point at it, and the maintenance middleware special-cases
	// the health prefix. Additive migration — nothing that probes today changes.
	r := NewRouter(Config{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil))

	if rec.Code == http.StatusNotFound {
		t.Fatal("GET /api/v1/health/deep = 404; removing it breaks every probe pointing at it today")
	}
}
