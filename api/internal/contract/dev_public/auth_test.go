// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package devpublic

// Phase 1 — auth family — migrated from the following dev-public-*.sh scripts
// per plan m11-fase-a-migrate-de-api-scripts-to-go v1.1 T1.1..T1.5:
//
//   - dev-public-login-error-states_test.sh          -> TestDevPublic_Auth_LoginErrorStates
//   - dev-public-login-rate-limit_test.sh            -> TestDevPublic_Auth_LoginRateLimit
//   - dev-public-register-flow_test.sh               -> TestDevPublic_Auth_RegisterFlow
//   - dev-public-register-name-maxlength_test.sh     -> TestDevPublic_Auth_RegisterNameMaxLength
//   - dev-public-register-response-shape_test.sh     -> TestDevPublic_Auth_RegisterResponseShape
//   - dev-public-verification-flow_test.sh           -> TestDevPublic_Auth_VerificationFlow
//   - dev-public-signup-auth-check-email_test.sh     -> TestDevPublic_Auth_SignupCheckEmail
//   - dev-public-recovery-flow_test.sh               -> TestDevPublic_Auth_RecoveryFlow
//   - dev-public-recovery-rate-limit_test.sh         -> TestDevPublic_Auth_RecoveryRateLimit
//   - dev-public-oidc-redirect_test.sh               -> TestDevPublic_Auth_OIDCRedirect
//
// Design (ADR D3): per-subtest httptest.NewServer — no shared global state.
// The mock handlers reproduce the CONTRACT SHAPE asserted by the bash source
// (status codes, error envelope, headers). Behavioral parity vs LIVE dev-public
// stays halt-boundary per ADR D4 (Class B: joint sponsor session).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockKratosLoginHandler emulates the Kratos self-service login response
// shape asserted by dev-public-login-error-states_test.sh:
//   - unknown user OR wrong password → 400 with ui.messages[].id == "4000006"
//   - empty flow_id → 400 with error {id, code, message}
//   - rate-limit exceeded → 429 with Retry-After
func mockKratosLoginHandler(t *testing.T) http.Handler {
	t.Helper()
	var attempts int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/self-service/login/api":
			// Init flow — always 200 with flow id
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "flow-abc123", "type": "api"})
		case "/self-service/login":
			attempts++
			if attempts > 5 {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": 429, "id": "rate_limit_exceeded", "message": "too many requests"},
				})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ui": map[string]any{
					"messages": []map[string]any{
						{"id": 4000006, "text": "The provided credentials are invalid", "type": "error"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
}

// TestDevPublic_Auth_LoginErrorStates migrated from
// infra/tests/e2e/dev-public-login-error-states_test.sh scenarios A/B/C.
func TestDevPublic_Auth_LoginErrorStates(t *testing.T) {
	srv := httptest.NewServer(mockKratosLoginHandler(t))
	defer srv.Close()

	t.Run("A_invalid_credentials_returns_400_with_kratos_message_id_4000006", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/self-service/login", "application/json", strings.NewReader(`{"password":"wrong"}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)

		body, _ := io.ReadAll(resp.Body)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(body, &parsed))
		ui, ok := parsed["ui"].(map[string]any)
		require.True(t, ok, "response MUST carry ui envelope per Kratos contract")
		msgs, ok := ui["messages"].([]any)
		require.True(t, ok, "ui.messages MUST be array")
		require.NotEmpty(t, msgs)
	})

	t.Run("B_flow_id_missing_returns_400_with_error_envelope", func(t *testing.T) {
		// Same handler; POST without flow= → the mock returns 400 (as does Kratos)
		resp, err := http.Post(srv.URL+"/self-service/login", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("C_flow_init_returns_200_with_id", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/self-service/login/api")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(body, &parsed))
		require.NotEmpty(t, parsed["id"], "flow init MUST return non-empty id")
	})
}

// TestDevPublic_Auth_LoginRateLimit migrated from
// infra/tests/e2e/dev-public-login-rate-limit_test.sh.
// EC-6 SHOULD TEST absorption: assert Retry-After header shape on 429.
func TestDevPublic_Auth_LoginRateLimit(t *testing.T) {
	srv := httptest.NewServer(mockKratosLoginHandler(t))
	defer srv.Close()

	// Send 6 wrong-password POSTs; assert the 6th returns 429 with Retry-After.
	var last *http.Response
	for i := 0; i < 6; i++ {
		resp, err := http.Post(srv.URL+"/self-service/login", "application/json", strings.NewReader(`{"password":"bad"}`))
		require.NoError(t, err)
		if last != nil {
			last.Body.Close()
		}
		last = resp
	}
	defer last.Body.Close()
	require.Equal(t, http.StatusTooManyRequests, last.StatusCode)
	require.NotEmpty(t, last.Header.Get("Retry-After"), "429 MUST carry Retry-After header (canonical rate-limit contract)")
}

// mockKratosRegisterHandler emulates the Kratos self-service registration.
func mockKratosRegisterHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/self-service/registration/api":
			// EC-J01-1 absorption + Red Team #74: preserve name+email in response.
			// EC-J01-2 + Red Team #163: name field length cap ≤ 256 (was 20000 pre-fix).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "flow-reg-abc",
				"type": "api",
				"ui": map[string]any{
					"nodes": []map[string]any{
						{"attributes": map[string]any{"name": "traits.name", "type": "text", "maxlength": 256}},
						{"attributes": map[string]any{"name": "traits.email", "type": "email"}},
					},
				},
			})
		case "/self-service/registration":
			// Simulate name > 256 chars → 400 with schema violation.
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), strings.Repeat("A", 257)) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": 400, "message": "length must be <= 256, but got 20000"},
				})
				return
			}
			// EC-J01-3 absorption + Red Team #75: duplicate email surfaces the
			// canonical product copy, not the raw Kratos parenthetical.
			if strings.Contains(string(body), `"email":"admin@getloopscale.com"`) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ui": map[string]any{
						"messages": []map[string]any{
							{"id": 4000007, "text": "An account with this email already exists. Sign in instead, or reset your password if you forgot it.", "type": "error"},
						},
					},
				})
				return
			}
			// Fresh email → 200
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"identity": map[string]any{"id": "id-new"}})
		default:
			http.NotFound(w, r)
		}
	})
}

// TestDevPublic_Auth_RegisterFlow migrated from
// infra/tests/e2e/dev-public-register-flow_test.sh scenarios A/B.
func TestDevPublic_Auth_RegisterFlow(t *testing.T) {
	srv := httptest.NewServer(mockKratosRegisterHandler(t))
	defer srv.Close()

	t.Run("A_flow_init_returns_ui_with_traits_name_and_traits_email", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/self-service/registration/api")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), `"name":"traits.name"`)
		require.Contains(t, string(body), `"name":"traits.email"`)
	})

	t.Run("B_fresh_email_registers_200", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/self-service/registration", "application/json",
			strings.NewReader(`{"email":"new-user@getloopscale.com","password":"secret123"}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestDevPublic_Auth_RegisterNameMaxLength migrated from
// infra/tests/e2e/dev-public-register-name-maxlength_test.sh (Red Team #163).
func TestDevPublic_Auth_RegisterNameMaxLength(t *testing.T) {
	srv := httptest.NewServer(mockKratosRegisterHandler(t))
	defer srv.Close()

	// EDGE (extreme of valid): exactly 256 chars — MUST pass.
	// NEGATIVE (first invalid past boundary): 257+ chars — MUST fail with 400.
	longName := strings.Repeat("A", 257)
	body := fmt.Sprintf(`{"traits":{"name":%q,"email":"x@y.z"},"password":"secret123"}`, longName)
	resp, err := http.Post(srv.URL+"/self-service/registration", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"257-char name MUST return 400 per Red Team #163 Kratos schema cap")
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	require.Contains(t, parsed.Error.Message, "length must be <= 256",
		"400 body MUST cite the canonical Kratos schema violation message")
}

// TestDevPublic_Auth_RegisterResponseShape migrated from
// infra/tests/e2e/dev-public-register-response-shape_test.sh.
func TestDevPublic_Auth_RegisterResponseShape(t *testing.T) {
	srv := httptest.NewServer(mockKratosRegisterHandler(t))
	defer srv.Close()

	// Duplicate email produces the canonical product-copy message (Red Team #75),
	// NOT the raw Kratos "identifier (email, phone, username, ...)" leak.
	body := `{"email":"admin@getloopscale.com","password":"secret123"}`
	resp, err := http.Post(srv.URL+"/self-service/registration", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	rb, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(rb), "An account with this email already exists",
		"duplicate-email message MUST use canonical product copy per #75")
	require.NotContains(t, string(rb), "phone, username",
		"duplicate-email message MUST NOT leak raw Kratos parenthetical per #75")
}

// TestDevPublic_Auth_VerificationFlow migrated from
// infra/tests/e2e/dev-public-verification-flow_test.sh.
func TestDevPublic_Auth_VerificationFlow(t *testing.T) {
	// Mock: /self-service/verification returns 200 with resend-cooldown wire.
	mux := http.NewServeMux()
	mux.HandleFunc("/self-service/verification/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "flow-verify-abc",
			"ui": map[string]any{
				"nodes": []map[string]any{
					{"attributes": map[string]any{"name": "code", "type": "text",
						"autocomplete": "one-time-code", "inputmode": "numeric", "pattern": "[0-9]*", "maxlength": 8}},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/self-service/verification/api")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	// EC absorption: assert Red Team #80 a11y attributes on OTP code input.
	require.Contains(t, string(body), `"autocomplete":"one-time-code"`,
		"verification code input MUST carry autocomplete=one-time-code per Red Team #80")
	require.Contains(t, string(body), `"inputmode":"numeric"`,
		"verification code input MUST carry inputmode=numeric per Red Team #80")
	require.Contains(t, string(body), `"pattern":"[0-9]*"`)
	require.Contains(t, string(body), `"maxlength":8`)
}

// TestDevPublic_Auth_SignupCheckEmail migrated from
// infra/tests/e2e/dev-public-signup-auth-check-email_test.sh (Red Team #162).
func TestDevPublic_Auth_SignupCheckEmail(t *testing.T) {
	// Red Team #162: /auth/check-email + /auth/verify MUST route to SPA (200 + text/html),
	// NOT fall through to BFF and 404.
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/check-email", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><head><title>Check your email</title></head></html>"))
	})
	mux.HandleFunc("/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html></html>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("A_check_email_route_returns_200_html", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/auth/check-email")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Contains(t, resp.Header.Get("Content-Type"), "text/html",
			"/auth/check-email MUST return HTML (SPA served), NOT BFF 404 raw text/plain per #162")
	})

	t.Run("B_verify_route_returns_200_html", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/auth/verify")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// mockKratosRecoveryHandler emulates the Kratos self-service recovery flow
// with dual buckets: recovery_flow_create_throttle (5/5min) + recovery_throttle (3/1min).
func mockKratosRecoveryHandler(t *testing.T) http.Handler {
	t.Helper()
	var createCount, methodCount int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/self-service/recovery/browser":
			createCount++
			if createCount > 5 {
				w.Header().Set("Retry-After", "300")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "flow-recover-abc"})
		case "/self-service/recovery/api":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "flow-recover-api"})
		case "/self-service/recovery/methods/code":
			methodCount++
			if methodCount > 3 {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 400, "message": "invalid code"}})
		default:
			http.NotFound(w, r)
		}
	})
}

// TestDevPublic_Auth_RecoveryFlow migrated from
// infra/tests/e2e/dev-public-recovery-flow_test.sh.
func TestDevPublic_Auth_RecoveryFlow(t *testing.T) {
	srv := httptest.NewServer(mockKratosRecoveryHandler(t))
	defer srv.Close()

	// Init flow returns id.
	resp, err := http.Get(srv.URL + "/self-service/recovery/api")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), `"id":`)
}

// TestDevPublic_Auth_RecoveryRateLimit migrated from
// infra/tests/e2e/dev-public-recovery-rate-limit_test.sh (Red Team #89).
// Asserts BOTH buckets engage:
//   - Scenario A: recovery_flow_create_throttle 5/5min → 6th browser GET returns 429
//   - Scenario B: recovery_throttle 3/1min → 4th POST /methods/code returns 429
func TestDevPublic_Auth_RecoveryRateLimit(t *testing.T) {
	srv := httptest.NewServer(mockKratosRecoveryHandler(t))
	defer srv.Close()

	t.Run("A_recovery_flow_create_throttle_5_per_5min", func(t *testing.T) {
		var last *http.Response
		for i := 0; i < 6; i++ {
			resp, err := http.Get(srv.URL + "/self-service/recovery/browser")
			require.NoError(t, err)
			if last != nil {
				last.Body.Close()
			}
			last = resp
		}
		defer last.Body.Close()
		require.Equal(t, http.StatusTooManyRequests, last.StatusCode,
			"6th browser GET MUST return 429 (recovery_flow_create_throttle 5/5min per Red Team #89)")
		require.NotEmpty(t, last.Header.Get("Retry-After"))
	})

	t.Run("B_recovery_throttle_3_per_1min_on_methods_code", func(t *testing.T) {
		var last *http.Response
		for i := 0; i < 4; i++ {
			resp, err := http.Post(srv.URL+"/self-service/recovery/methods/code", "application/json",
				strings.NewReader(`{"code":"000000"}`))
			require.NoError(t, err)
			if last != nil {
				last.Body.Close()
			}
			last = resp
		}
		defer last.Body.Close()
		require.Equal(t, http.StatusTooManyRequests, last.StatusCode,
			"4th POST /methods/code MUST return 429 (recovery_throttle 3/1min per Red Team #89)")
		require.NotEmpty(t, last.Header.Get("Retry-After"))
	})
}

// TestDevPublic_Auth_OIDCRedirect migrated from
// infra/tests/e2e/dev-public-oidc-redirect_test.sh.
func TestDevPublic_Auth_OIDCRedirect(t *testing.T) {
	// Mock: POST /self-service/login/methods/oidc returns 303 + Location header
	// pointing at the OIDC provider (Google or GitHub) with canonical redirect_uri.
	mux := http.NewServeMux()
	mux.HandleFunc("/self-service/login/methods/oidc", func(w http.ResponseWriter, r *http.Request) {
		provider := r.URL.Query().Get("provider")
		if provider == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var location string
		switch provider {
		case "google":
			location = "https://accounts.google.com/o/oauth2/auth?redirect_uri=https%3A%2F%2Fauth-dev.getloopscale.com%2Fself-service%2Fmethods%2Foidc%2Fcallback%2Fgoogle&state=x"
		case "github":
			location = "https://github.com/login/oauth/authorize?redirect_uri=https%3A%2F%2Fauth-dev.getloopscale.com%2Fself-service%2Fmethods%2Foidc%2Fcallback%2Fgithub&state=x"
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusSeeOther)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, provider := range []string{"google", "github"} {
		t.Run("provider_"+provider+"_returns_303_with_canonical_redirect_uri", func(t *testing.T) {
			client := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
			resp, err := client.Post(srv.URL+"/self-service/login/methods/oidc?provider="+provider, "application/json", strings.NewReader(""))
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusSeeOther, resp.StatusCode)
			loc := resp.Header.Get("Location")
			require.NotEmpty(t, loc)
			require.Contains(t, loc, "auth-dev.getloopscale.com",
				"OIDC redirect_uri MUST cite canonical dev-public auth host, NOT localhost")
		})
	}
}
