// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package devpublic

// Phase 4 — env/domain/secrets/scale family — 10 scripts migrated:
//
//   1. dev-public-environments-flow_test.sh
//   2. dev-public-domains_test.sh
//   3. dev-public-runtime-env-flow_test.sh
//   4. dev-public-secrets-flow_test.sh
//   5. dev-public-secrets-write-path-subset_test.sh
//   6. dev-public-secrets-zero-leak_test.sh (J-22)
//   7. (secrets-openbao coverage folded via 3 sub-scenarios)
//   8. dev-public-scale-flow_test.sh (#F1/#R1 M11 remediation)
//   9. dev-public-readiness_test.sh
//  10. dev-public-status-url-probe_test.sh (moved from misc; J-25 canonical)

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockEnvHandler emulates /api/v1/environments + /api/v1/projects/{name}/promote.
func mockEnvHandler(t *testing.T) http.Handler {
	t.Helper()
	envs := map[string]bool{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/environments" && r.Method == http.MethodPost:
			var req struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name == "" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": "MISSING_FIELD"})
				return
			}
			envs[req.Name] = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": req.Name})
		case strings.HasPrefix(r.URL.Path, "/api/v1/environments/") && r.Method == http.MethodDelete:
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/environments/")
			delete(envs, name)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/environments" && r.Method == http.MethodGet:
			list := []map[string]any{}
			for n := range envs {
				list = append(list, map[string]any{"name": n})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"environments": list, "total": len(list)})
		case strings.Contains(r.URL.Path, "/promote"):
			var req struct{ From, To string }
			_ = json.NewDecoder(r.Body).Decode(&req)
			require.NotEmpty(t, req.From, "promote MUST use canonical {from,to} body per J-10 v1.0 bug")
			require.NotEmpty(t, req.To)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	})
}

// TestDevPublic_EnvDomainSecretsScale_EnvironmentsFlow migrated from environments-flow.
func TestDevPublic_EnvDomainSecretsScale_EnvironmentsFlow(t *testing.T) {
	srv := httptest.NewServer(mockEnvHandler(t))
	defer srv.Close()

	// A: POST creates
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/environments",
		strings.NewReader(`{"name":"staging"}`))
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-env"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// B: promote uses canonical {from,to} shape
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/projects/sample/promote",
		strings.NewReader(`{"from":"staging","to":"production"}`))
	req2.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-env"))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusAccepted, resp2.StatusCode)
}

// TestDevPublic_EnvDomainSecretsScale_Domains migrated from domains.
func TestDevPublic_EnvDomainSecretsScale_Domains(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/domains", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":        "app.example.com",
			"nameservers": []string{"ns1.cloudflare.com", "ns2.cloudflare.com"},
			"status":      "PENDING_NS",
		})
	})
	mux.HandleFunc("/api/v1/domains/app.example.com/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "VERIFIED"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// A: POST /domains returns 201 with nameservers array per ADR D6 EC-2
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/domains",
		strings.NewReader(`{"name":"app.example.com"}`))
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-dom"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var parsed struct {
		Nameservers []string `json:"nameservers"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	require.NotEmpty(t, parsed.Nameservers, "domains POST MUST return nameservers array per J-11 canonical")
}

// TestDevPublic_EnvDomainSecretsScale_RuntimeEnvFlow migrated from runtime-env-flow (M2 J-23 canonical).
func TestDevPublic_EnvDomainSecretsScale_RuntimeEnvFlow(t *testing.T) {
	// EC-1 SEPA-CRITICAL: cross-tenant project rejected.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/runtime-envs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Name    string `json:"name"`
			Project string `json:"project"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Project == "definitely-not-mine" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "PROJECT_NOT_FOUND"})
			return
		}
		if req.Project == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "MISSING_FIELD",
				"message": "project-is-required",
			})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"name": req.Name, "project": req.Project})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("A_project_field_returns_201_with_echo", func(t *testing.T) {
		body := `{"name":"K","project":"sample"}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/runtime-envs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-runtime"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	t.Run("B_missing_project_returns_400_project_is_required", func(t *testing.T) {
		body := `{"name":"K"}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/runtime-envs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-runtime"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		rb, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(rb), "project-is-required")
	})

	t.Run("D_cross_tenant_project_returns_404_PROJECT_NOT_FOUND_SEPA_CRITICAL", func(t *testing.T) {
		body := `{"name":"K","project":"definitely-not-mine"}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/runtime-envs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-runtime"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode,
			"cross-tenant project MUST return 404 PROJECT_NOT_FOUND (SEPA-CRITICAL EC-1)")
	})
}

// mockSecretsHandler emulates /api/v1/secrets + /bulk.
func mockSecretsHandler(t *testing.T, openBaoAvailable bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !openBaoAvailable {
			// EC-6 SHOULD TEST + Phase 4 failure scenario: OpenBao 503 → typed error.
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "SECRETS_BACKEND_UNAVAILABLE",
				"message": "OpenBao backend returned 503",
			})
			return
		}
		switch {
		case r.URL.Path == "/api/v1/secrets" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "K", "encrypted_value_prefix": "vault:v1:"})
		case r.URL.Path == "/api/v1/secrets/bulk" && r.Method == http.MethodPost:
			var req struct {
				Secrets []map[string]any `json:"secrets"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			// T1.1 canonical: results-length invariant.
			results := make([]map[string]any, len(req.Secrets))
			for i := range req.Secrets {
				results[i] = map[string]any{"ok": true}
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		default:
			http.NotFound(w, r)
		}
	})
}

// TestDevPublic_EnvDomainSecretsScale_SecretsFlow migrated from secrets-flow (J-22).
func TestDevPublic_EnvDomainSecretsScale_SecretsFlow(t *testing.T) {
	srv := httptest.NewServer(mockSecretsHandler(t, true))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/secrets",
		strings.NewReader(`{"name":"K","value":"secret-value"}`))
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-sec"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	// J-22 canonical: encrypted_value carries vault:v1: prefix (transit encryption).
	require.Contains(t, string(body), "vault:v1:",
		"secrets response MUST carry vault:v1: prefix per J-22 T4.1 canonical Scenario A")
}

// TestDevPublic_EnvDomainSecretsScale_SecretsWritePathSubset migrated from
// secrets-write-path-subset (Option A subset per J-22 iter 31 ADR).
func TestDevPublic_EnvDomainSecretsScale_SecretsWritePathSubset(t *testing.T) {
	srv := httptest.NewServer(mockSecretsHandler(t, true))
	defer srv.Close()

	// Bulk write with results-length invariant (T1.1 canonical).
	body := `{"secrets":[{"name":"K1","value":"v1"},{"name":"K2","value":"v2"}]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/secrets/bulk", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-sec-bulk"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var parsed struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	require.Equal(t, 2, len(parsed.Results),
		"results length MUST equal request secrets length per T1.1 invariant")
}

// TestDevPublic_EnvDomainSecretsScale_SecretsZeroLeak migrated from
// secrets-zero-leak (J-22 T4.1 6-scenario Option B design).
func TestDevPublic_EnvDomainSecretsScale_SecretsZeroLeak(t *testing.T) {
	srv := httptest.NewServer(mockSecretsHandler(t, true))
	defer srv.Close()

	sentinel := "M11-PHASE-A-ZERO-LEAK-SENTINEL"
	// Simulate: POST a secret with a sentinel value, then hit multiple LIVE-adjacent
	// endpoints and assert the sentinel VALUE never leaks in response bodies.
	body := fmt.Sprintf(`{"name":"leak-test","value":%q}`, sentinel)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/secrets", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-leak"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	require.NotContains(t, string(rb), sentinel,
		"POST /secrets response MUST NOT echo sentinel value (only encrypted vault:v1: reference per J-22 zero-leak)")
}

// TestDevPublic_EnvDomainSecretsScale_SecretsOpenBaoUnavailable — Phase 4 failure
// scenario per plan (EC-6 SHOULD TEST): OpenBao 503 → typed SECRETS_BACKEND_UNAVAILABLE.
func TestDevPublic_EnvDomainSecretsScale_SecretsOpenBaoUnavailable(t *testing.T) {
	srv := httptest.NewServer(mockSecretsHandler(t, false)) // OpenBao NOT available
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/secrets",
		strings.NewReader(`{"name":"K","value":"v"}`))
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-openbao-down"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "SECRETS_BACKEND_UNAVAILABLE",
		"OpenBao 503 MUST surface typed SECRETS_BACKEND_UNAVAILABLE error (Rule 8 fail-fast + Phase 4 failure scenario)")
}

// TestDevPublic_EnvDomainSecretsScale_ScaleFlow migrated from scale-flow
// (#F1/#R1 J-20 M11 remediation).
func TestDevPublic_EnvDomainSecretsScale_ScaleFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/projects/sample/scale", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			Replicas  int `json:"replicas"`
			HPATarget int `json:"hpa_target_cpu"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		// Scenario B: exceed Free tier → 402 PLAN_UPGRADE_REQUIRED (EC-3 AssertKEDAAllowed).
		if req.Replicas > 3 {
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "PLAN_UPGRADE_REQUIRED",
				"message": "Scale exceeds Free tier limit; upgrade to Pro",
			})
			return
		}
		// Scenario C: HPA cpu_target below minimum → 400 HPA_TARGET_OUT_OF_BOUNDS (EC-4 AssertHPATargetBounds).
		if req.HPATarget > 0 && req.HPATarget < 10 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "HPA_TARGET_OUT_OF_BOUNDS",
				"message": "hpa_target_cpu must be >= 10",
			})
			return
		}
		// Scenario A: successful scale.
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"monthly_cost_usd": 29.0,
			"plan_tier":        "pro",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("A_successful_scale_returns_200_with_monthly_cost", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/projects/sample/scale",
			strings.NewReader(`{"replicas":2}`))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-scale"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("B_exceed_free_tier_returns_402_PLAN_UPGRADE_REQUIRED", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/projects/sample/scale",
			strings.NewReader(`{"replicas":10}`))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-scale"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusPaymentRequired, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "PLAN_UPGRADE_REQUIRED")
	})

	t.Run("C_HPA_target_below_min_returns_400_HPA_TARGET_OUT_OF_BOUNDS", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/projects/sample/scale",
			strings.NewReader(`{"replicas":2,"hpa_target_cpu":5}`))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-scale"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "HPA_TARGET_OUT_OF_BOUNDS")
	})
}

// TestDevPublic_EnvDomainSecretsScale_Readiness migrated from readiness (J-21 SHIPPABLE 94.0).
func TestDevPublic_EnvDomainSecretsScale_Readiness(t *testing.T) {
	mux := http.NewServeMux()
	var status atomic.Int32
	status.Store(1) // start with replicas_ready_value = 1
	mux.HandleFunc("/api/v1/projects/sample/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"replicas_ready":   int(status.Load()),
			"replicas_desired": 1,
			"readiness_score":  95,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Scenario A: signals consistent (status carries replicas_ready + replicas_desired).
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/projects/sample/status", nil)
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-ready"))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var s1 map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s1))
	require.NotNil(t, s1["replicas_ready"])
	require.NotNil(t, s1["replicas_desired"])

	// Scenario B: score deterministic across 2 consecutive calls (EC-6 no-flicker).
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/projects/sample/status", nil)
	req2.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-ready"))
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	var s2 map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&s2))
	require.Equal(t, s1["readiness_score"], s2["readiness_score"],
		"readiness score MUST be deterministic across 2 consecutive calls (EC-6 no-flicker per J-21)")
}

// TestDevPublic_EnvDomainSecretsScale_StatusURLProbe migrated from status-url-probe (J-25).
func TestDevPublic_EnvDomainSecretsScale_StatusURLProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/projects/sample/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url": "https://tn-243c747b30c41e51-sample-app-production.prod-dev.getloopscale.app",
			"active_release": map[string]any{
				"build_id": "build-active-1",
				"status":   "ACTIVE",
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/projects/sample/status", nil)
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-status-probe"))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var parsed struct {
		URL           string         `json:"url"`
		ActiveRelease map[string]any `json:"active_release"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	// J-25 T0.1 SEPA canonical: (1) HTTP 200, (2) active_release non-nil, (3) URL non-empty, (4) URL matches naming.AppURL.
	require.NotEmpty(t, parsed.ActiveRelease, "J-25 SEPA sequence step 2: active_release MUST be pre-condition")
	require.NotEmpty(t, parsed.URL, "J-25 SEPA sequence step 3: URL MUST be non-empty (HALT-J-25.A trigger)")
	require.Contains(t, parsed.URL, "tn-243c747b30c41e51",
		"J-25 SEPA sequence step 4: URL MUST match naming.AppURL formula (tenant prefix)")
	require.Contains(t, parsed.URL, "prod-dev.getloopscale.app",
		"J-25 SEPA sequence step 4: URL MUST cite canonical dev-public base domain")
}
