// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/consent"
)

// mockOryServer simulates the Hydra admin + Kratos admin/public endpoints the
// consent handler talks to. consentClientID is the client_id returned for a
// consent challenge. identityHits counts /admin/identities calls so tests can
// assert which admin URL was used.
func mockOryServer(t *testing.T, consentClientID string, identityHits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/admin/oauth2/auth/requests/consent/accept"):
			_ = json.NewEncoder(w).Encode(map[string]string{"redirect_to": "https://hydra/continue"})
		case strings.HasPrefix(r.URL.Path, "/admin/oauth2/auth/requests/consent"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"client_id":       consentClientID,
				"subject":         "kratos-user-1",
				"requested_scope": []string{"openid", "offline_access"},
			})
		case strings.HasPrefix(r.URL.Path, "/admin/identities/"):
			if identityHits != nil {
				atomic.AddInt32(identityHits, 1)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"traits": map[string]string{"email": "user@example.com", "name": "User"},
			})
		case strings.HasPrefix(r.URL.Path, "/sessions/whoami"):
			// No active session — forces DeviceVerify into the login-redirect branch.
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newConsentHandler(srvURL string) *consent.Handler {
	logger := slog.New(slog.NewTextHandler(httptest.NewRecorder().Body, nil))
	hydra := auth.NewHydraClient(srvURL, srvURL, "loopzero-cli")
	prov := auth.NewTenantProvisioner(nil, logger)
	// kratosPublic, kratosInternal, kratosAdmin all point at the mock server.
	return consent.NewHandler(hydra, prov, srvURL, srvURL, srvURL, logger)
}

// T1.1 — consent must reject any client_id that is not the trusted first-party client.
func TestConsent_RejectsUntrustedClient(t *testing.T) {
	srv := mockOryServer(t, "evil-client", nil)
	defer srv.Close()
	h := newConsentHandler(srv.URL)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/consent?consent_challenge=chal-1", nil)
	h.Consent(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for untrusted client, got %d", rec.Code)
	}
}

// T1.1 — consent auto-approves the trusted first-party client.
func TestConsent_AllowsTrustedClient(t *testing.T) {
	srv := mockOryServer(t, "loopzero-cli", nil)
	defer srv.Close()
	h := newConsentHandler(srv.URL)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/consent?consent_challenge=chal-1", nil)
	h.Consent(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect for trusted client, got %d", rec.Code)
	}
}

// T1.2 — identity lookup must use the configured Kratos admin URL, not a
// string-replace heuristic on the public URL.
func TestConsent_UsesConfiguredKratosAdminURL(t *testing.T) {
	var hits int32
	srv := mockOryServer(t, "loopzero-cli", &hits)
	defer srv.Close()
	// kratosInternalURL contains "kratos-public" so the OLD string-replace
	// heuristic would rewrite it to an unreachable "kratos-admin" host (0 hits).
	// The new code uses the explicitly configured kratosAdminURL (the mock),
	// so the identity endpoint IS hit.
	logger := slog.New(slog.NewTextHandler(httptest.NewRecorder().Body, nil))
	hydra := auth.NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	prov := auth.NewTenantProvisioner(nil, logger)
	h := consent.NewHandler(hydra, prov, "https://login.example", "http://kratos-public.invalid", srv.URL, logger)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/consent?consent_challenge=chal-1", nil)
	h.Consent(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("expected identity lookup to hit the configured Kratos admin URL, got 0 calls")
	}
}

// T1.5 — device-verify return_to must be a relative /auth/device path, never an
// attacker-controlled absolute URL (open-redirect guard).
func TestDeviceVerify_ReturnToIsRelativePath(t *testing.T) {
	srv := mockOryServer(t, "loopzero-cli", nil)
	defer srv.Close()
	h := newConsentHandler(srv.URL)

	rec := httptest.NewRecorder()
	// Craft a request with an absolute host in the URL — must not leak into return_to.
	req := httptest.NewRequest("GET", "https://evil.example/auth/device?device_challenge=dc1&user_code=ABCD-1234", nil)
	h.DeviceVerify(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect to login, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	rt := u.Query().Get("return_to")
	if !strings.HasPrefix(rt, "/auth/device") {
		t.Fatalf("return_to must be a relative /auth/device path, got %q", rt)
	}
	if strings.Contains(rt, "evil.example") || strings.Contains(rt, "https://") {
		t.Fatalf("return_to leaked an absolute URL: %q", rt)
	}
}
