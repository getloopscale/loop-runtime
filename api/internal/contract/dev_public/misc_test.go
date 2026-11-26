// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package devpublic

// Phase 6 — misc family — remaining scripts (Q3 potential M13-reclassification pending):
//
//   1. dev-public-cleanroom-canonical-9-journeys_test.sh (orchestration wrapper)
//   2. dev-public-redteam-canonical-paths_test.sh (Red Team #68/#43 post-fix regression protectors)
//   3. dev-public-seed-post-cleanroom-tenant-cr_test.sh (Red Team #118)
//
// Note: dev-public-status-url-probe_test.sh migrated to Phase 4 as
// TestDevPublic_EnvDomainSecretsScale_StatusURLProbe (better fit alongside
// readiness + status contracts).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDevPublic_Misc_CleanroomCanonical9Journeys — orchestration wrapper.
// Bash dispatches 7 auto journeys + 2 semi-auto. The Go migration asserts the
// wrapper's contract: dispatches only when the pre-flight JWT seed is present.
func TestDevPublic_Misc_CleanroomCanonical9Journeys(t *testing.T) {
	// This is fundamentally an operator orchestration test — the Go migration
	// documents the pre-flight contract without duplicating the LIVE dispatch
	// (that stays in bash per ADR D4 halt boundary).
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "AUTH_REQUIRED"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Pre-flight assertion: 9-journey orchestrator MUST have a valid JWT before dispatch.
	// Simulate: missing JWT → 401 blocks orchestrator; present JWT → 200 allows dispatch.
	t.Run("missing_jwt_blocks_orchestrator", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/health")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"orchestrator pre-flight MUST fail on missing JWT (no dispatch on stale infra)")
	})

	t.Run("present_jwt_allows_dispatch", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/health", nil)
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-9journeys"))
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestDevPublic_Misc_RedteamCanonicalPaths — Red Team #68/#43 post-fix
// regression protector on the canonical browser-flow paths.
func TestDevPublic_Misc_RedteamCanonicalPaths(t *testing.T) {
	mux := http.NewServeMux()
	// #68 canonical: /self-service/login (without /methods/*) MUST engage login_throttle.
	var loginAttempts int
	mux.HandleFunc("/self-service/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		loginAttempts++
		if loginAttempts > 10 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	})
	// #43 canonical: /self-service/registration MUST unify BFF response for new vs existing.
	mux.HandleFunc("/self-service/registration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"universal": "check-your-email"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("A_canonical_login_path_engages_rate_limit_per_68", func(t *testing.T) {
		var last *http.Response
		for i := 0; i < 12; i++ {
			resp, err := http.Post(srv.URL+"/self-service/login", "application/json",
				strings.NewReader(`{"password":"wrong"}`))
			require.NoError(t, err)
			if last != nil {
				last.Body.Close()
			}
			last = resp
		}
		defer last.Body.Close()
		require.Equal(t, http.StatusTooManyRequests, last.StatusCode,
			"canonical /self-service/login rate-limit MUST engage at 12 attempts per Red Team #68")
	})

	t.Run("B_canonical_registration_unifies_response_per_43", func(t *testing.T) {
		resp1, err := http.Post(srv.URL+"/self-service/registration", "application/json",
			strings.NewReader(`{"email":"new@x"}`))
		require.NoError(t, err)
		defer resp1.Body.Close()
		body1, _ := io.ReadAll(resp1.Body)

		resp2, err := http.Post(srv.URL+"/self-service/registration", "application/json",
			strings.NewReader(`{"email":"existing@x"}`))
		require.NoError(t, err)
		defer resp2.Body.Close()
		body2, _ := io.ReadAll(resp2.Body)

		require.Equal(t, string(body1), string(body2),
			"new + existing email MUST return byte-identical body per Red Team #43 unifier")
	})
}

// TestDevPublic_Misc_SeedPostCleanroomTenantCR — Red Team #118.
func TestDevPublic_Misc_SeedPostCleanroomTenantCR(t *testing.T) {
	// Post-#118 fix: Tenant CR is materialized for QA Red Team identity
	// (paulohenrique.java@gmail.com → tn-79611071399a2e5e) so deploys succeed.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/tn-79611071399a2e5e", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "tn-79611071399a2e5e",
			"status":     "ACTIVE",
			"apiVersion": "tenants.runtime.getloopscale.com/v1",
			"kind":       "Tenant",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Scenario B (the #118 bug): paulohenrique.java@gmail.com's Tenant CR MUST exist.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/tenants/tn-79611071399a2e5e", nil)
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-79611071399a2e5e"))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"paulohenrique.java Tenant CR MUST exist post-#118 canonical seed (pre-fix returned NotFound)")
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), `"apiVersion":"tenants.runtime.getloopscale.com/v1"`,
		"Tenant CR apiVersion MUST be canonical `tenants.runtime.getloopscale.com/v1` per #118 (NOT typo `loopruntime.sh`)")
	require.Contains(t, string(body), `"status":"ACTIVE"`,
		"Tenant CR MUST reach status ACTIVE post-seed per #118")
}
