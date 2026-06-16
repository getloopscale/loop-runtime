// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.1 + T1.3 of plan 2026-05-27-backend-frontend-integration v1.1.
//
// Covers the Kratos session → JWT exchange handler at
// cloud/internal/routes/session_token.go (zero test coverage before this
// file). Includes the T1.3 roundtrip — mint via Exchange handler, then
// validate via SessionTokenIssuer — proving the BFF JWT chain is
// internally consistent.
//
// Replaces plan v1.1 EC-02 / EC-05 with the *real* contract observed in
// the existing handler:
//   - EC-02 (zero-teams branch): NOT applicable here. TenantProvisioner
//     auto-provisions a starter tenant on first login (tenant.go:54-167),
//     so "no team" is impossible by construction; UX is one-step register
//     instead of two-step + onboarding redirect. Better than plan v1.1
//     proposed. TestExchange_NewUser_AutoProvisionsTenant pins this.
//   - EC-05 (configurable cookie name): NOT applicable here. The handler
//     forwards r.Cookies() verbatim to Kratos /sessions/whoami, so
//     whatever cookie name Kratos itself uses is honored automatically.
//     TestExchange_ForwardsArbitraryCookieNames pins this.

package unit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/routes"
	loopzerojwt "github.com/getloopscale/loopzero-contracts/jwt"
)

// mockKratosServer simulates Kratos /sessions/whoami. behavior is configured
// per test:
//   - statusCode: HTTP status to return (200, 401, 500, etc.)
//   - active:     "active" field in body (Kratos returns inactive for expired)
//   - bodyJSON:   custom body (overrides default identity)
//   - delay:      simulate slow Kratos for timeout tests
//   - capturedCookies: collects cookie names received (parallel-safe).
type mockKratosCfg struct {
	statusCode int
	active     bool
	identity   struct {
		ID    string
		Email string
		Name  string
	}
	delay           time.Duration
	capturedCookies *[]string
	mu              *sync.Mutex
}

func newMockKratosServer(t *testing.T, cfg mockKratosCfg) *httptest.Server {
	t.Helper()
	if cfg.mu == nil {
		cfg.mu = &sync.Mutex{}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			http.NotFound(w, r)
			return
		}
		if cfg.delay > 0 {
			time.Sleep(cfg.delay)
		}
		if cfg.capturedCookies != nil {
			cfg.mu.Lock()
			for _, c := range r.Cookies() {
				*cfg.capturedCookies = append(*cfg.capturedCookies, c.Name)
			}
			cfg.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		if cfg.statusCode != 0 && cfg.statusCode != http.StatusOK {
			w.WriteHeader(cfg.statusCode)
			json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
			return
		}
		w.WriteHeader(http.StatusOK)
		body := map[string]any{
			"active": cfg.active,
			"identity": map[string]any{
				"id": cfg.identity.ID,
				"traits": map[string]string{
					"email": cfg.identity.Email,
					"name":  cfg.identity.Name,
				},
			},
		}
		json.NewEncoder(w).Encode(body)
	}))
}

// buildHandler wires a SessionTokenHandler against a mock Kratos and an
// in-process SessionTokenIssuer + nil-db TenantProvisioner (returns
// "dev-tenant"/"owner" per tenant.go:36-38).
func buildHandler(t *testing.T, kratos *httptest.Server) (*routes.SessionTokenHandler, *loopzerojwt.SessionTokenIssuer) {
	t.Helper()
	issuer := loopzerojwt.NewSessionTokenIssuer(
		"test-secret-at-least-32-bytes-long-ok!",
		"loopzero-cloud-test",
		"loopzero-engine",
		3600*time.Second,
	)
	prov := auth.NewTenantProvisioner(nil, slog.Default()) // nil DB → "dev-tenant"
	return routes.NewSessionTokenHandler(issuer, prov, kratos.URL), issuer
}

func postWithCookie(t *testing.T, handler *routes.SessionTokenHandler, cookies map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/session/token", nil)
	for name, value := range cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	w := httptest.NewRecorder()
	handler.Exchange(w, req)
	return w
}

func TestExchange_HappyPath_ReturnsJWT(t *testing.T) {
	cfg := mockKratosCfg{statusCode: 200, active: true}
	cfg.identity.ID = "kratos-user-id-001"
	cfg.identity.Email = "alice@loopzero.dev"
	cfg.identity.Name = "Alice"
	kratos := newMockKratosServer(t, cfg)
	defer kratos.Close()

	handler, _ := buildHandler(t, kratos)

	w := postWithCookie(t, handler, map[string]string{
		"ory_kratos_session": "valid-opaque-cookie",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Errorf("access_token missing in response")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", resp.TokenType)
	}
	if resp.ExpiresIn != 3600 {
		t.Errorf("expires_in = %d, want 3600", resp.ExpiresIn)
	}
}

func TestExchange_RoundTrip_MintThenValidate(t *testing.T) {
	// T1.3: mint via Exchange → validate via SessionTokenIssuer.
	// Proves the JWT shape minted by the BFF is the same shape engine
	// auth middleware accepts (single source of truth: pkg/jwt).
	cfg := mockKratosCfg{statusCode: 200, active: true}
	cfg.identity.ID = "kratos-user-id-002"
	cfg.identity.Email = "bob@loopzero.dev"
	cfg.identity.Name = "Bob"
	kratos := newMockKratosServer(t, cfg)
	defer kratos.Close()

	handler, issuer := buildHandler(t, kratos)

	w := postWithCookie(t, handler, map[string]string{
		"ory_kratos_session": "any-value",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("mint: want 200, got %d", w.Code)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	claims, err := issuer.ValidateSession(resp.AccessToken)
	if err != nil {
		t.Fatalf("validate minted JWT: %v", err)
	}
	if claims.Subject != "kratos-user-id-002" {
		t.Errorf("subject = %q, want kratos-user-id-002", claims.Subject)
	}
	if claims.Email != "bob@loopzero.dev" {
		t.Errorf("email = %q", claims.Email)
	}
	if claims.TenantID == "" {
		t.Errorf("tenant_id missing — handler must always populate it")
	}
}

func TestExchange_Kratos401_Returns401(t *testing.T) {
	cfg := mockKratosCfg{statusCode: 401}
	kratos := newMockKratosServer(t, cfg)
	defer kratos.Close()

	handler, _ := buildHandler(t, kratos)
	w := postWithCookie(t, handler, map[string]string{"ory_kratos_session": "expired"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "AUTH_REQUIRED") {
		t.Errorf("want AUTH_REQUIRED code, got: %s", w.Body.String())
	}
}

func TestExchange_InactiveSession_Returns401(t *testing.T) {
	// Kratos may respond 200 but with active=false (e.g., session expired
	// but cookie not yet purged). Handler must treat this as 401.
	cfg := mockKratosCfg{statusCode: 200, active: false}
	cfg.identity.ID = "user-x"
	cfg.identity.Email = "expired@loopzero.dev"
	kratos := newMockKratosServer(t, cfg)
	defer kratos.Close()

	handler, _ := buildHandler(t, kratos)
	w := postWithCookie(t, handler, map[string]string{"ory_kratos_session": "stale"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExchange_KratosUnreachable_Returns502(t *testing.T) {
	// Point handler at a dead address. Net.DialContext fails fast → 502.
	issuer := loopzerojwt.NewSessionTokenIssuer("test-secret-32-bytes-blah-ok-blah-12", "i", "a", 3600*time.Second)
	prov := auth.NewTenantProvisioner(nil, slog.Default())
	// Reserved address (RFC 5737) — guaranteed unroutable.
	handler := routes.NewSessionTokenHandler(issuer, prov, "http://192.0.2.1:1") // TEST-NET-1

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/session/token", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "any"})
	w := httptest.NewRecorder()
	handler.Exchange(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "AUTH_SERVICE_UNAVAILABLE") {
		t.Errorf("want AUTH_SERVICE_UNAVAILABLE, got: %s", w.Body.String())
	}
}

func TestExchange_ForwardsArbitraryCookieNames(t *testing.T) {
	// Substitute for plan EC-05 (configurable cookie name): the handler
	// forwards all inbound cookies to Kratos, so any name Kratos itself
	// recognises works without env-var configuration.
	captured := []string{}
	cfg := mockKratosCfg{statusCode: 200, active: true, capturedCookies: &captured}
	cfg.identity.ID = "u"
	cfg.identity.Email = "c@d.e"
	kratos := newMockKratosServer(t, cfg)
	defer kratos.Close()

	handler, _ := buildHandler(t, kratos)
	w := postWithCookie(t, handler, map[string]string{
		"custom_kratos_name": "v1",
		"csrf_token":         "v2",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	got := map[string]bool{}
	for _, n := range captured {
		got[n] = true
	}
	if !got["custom_kratos_name"] || !got["csrf_token"] {
		t.Errorf("expected both cookies forwarded; got %v", captured)
	}
}

func TestExchange_NewUser_AutoProvisionsTenant(t *testing.T) {
	// Substitute for plan EC-02 (zero-teams flow): TenantProvisioner
	// auto-creates a starter tenant on first login, so the response
	// always carries a tenant_id and the frontend never needs an
	// onboarding-redirect branch. nil-DB provisioner returns "dev-tenant".
	cfg := mockKratosCfg{statusCode: 200, active: true}
	cfg.identity.ID = "brand-new-user"
	cfg.identity.Email = "brand-new@loopzero.dev"
	kratos := newMockKratosServer(t, cfg)
	defer kratos.Close()

	handler, issuer := buildHandler(t, kratos)
	w := postWithCookie(t, handler, map[string]string{"ory_kratos_session": "any"})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (auto-provision), got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	claims, err := issuer.ValidateSession(resp.AccessToken)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.TenantID != "dev-tenant" {
		t.Errorf("nil-DB auto-provision = %q, want dev-tenant", claims.TenantID)
	}
}

func TestExchange_RateLimit_Exceeded_Returns429(t *testing.T) {
	// In-memory rate limiter caps at 10 per minute per IP. The 11th hit
	// from the SAME RemoteAddr must respond 429 with standard headers.
	cfg := mockKratosCfg{statusCode: 200, active: true}
	cfg.identity.ID = "x"
	cfg.identity.Email = "x@loopzero.dev"
	kratos := newMockKratosServer(t, cfg)
	defer kratos.Close()

	handler, _ := buildHandler(t, kratos)

	var lastCode int
	for i := 0; i < 11; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/session/token", nil)
		req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "v"})
		// Pin RemoteAddr so the limiter uses one key across all iterations.
		req.RemoteAddr = "10.0.0.42:1234"
		w := httptest.NewRecorder()
		handler.Exchange(w, req)
		lastCode = w.Code
		if w.Code == http.StatusTooManyRequests {
			// Limiter emits X-RateLimit-* headers (cloud/internal/ratelimit/limiter.go:82-86).
			if w.Header().Get("X-RateLimit-Limit") == "" {
				t.Errorf("X-RateLimit-Limit header missing on 429")
			}
			return // First 429 is enough.
		}
	}
	t.Fatalf("limiter did not trip after 11 calls; last code = %d", lastCode)
}

func TestExchange_MalformedKratosBody_Returns401(t *testing.T) {
	// Kratos response that isn't valid JSON OR has unexpected shape.
	// Handler must NOT 500 — that would leak server internals to a
	// curious client.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "{this is not json}")
	}))
	defer srv.Close()

	issuer := loopzerojwt.NewSessionTokenIssuer("test-secret-32-bytes-blah-ok-blah-12", "i", "a", 3600*time.Second)
	prov := auth.NewTenantProvisioner(nil, slog.Default())
	handler := routes.NewSessionTokenHandler(issuer, prov, srv.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/session/token", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "v"})
	w := httptest.NewRecorder()
	handler.Exchange(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 (graceful), got %d", w.Code)
	}
}
