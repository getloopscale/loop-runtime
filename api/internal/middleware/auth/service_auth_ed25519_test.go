// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/getloopscale/loopzero-contracts/serviceauth"
)

// signedHeaders is a test helper that mints the three new headers for a
// given user/tenant + key. Mirrors what the cloud proxy will do in
// production. Centralised here so every test can reuse the exact
// canonical-payload construction.
func signedHeaders(t *testing.T, priv ed25519.PrivateKey, userID, tenantID, method, path string, ts time.Time, nonce string) (sig, tsStr, n string) {
	t.Helper()
	in := serviceauth.PayloadInput{
		UserID: userID, TenantID: tenantID, Timestamp: ts.Unix(),
		Nonce: nonce, Method: method, Path: path,
	}
	s, err := serviceauth.Sign(priv, in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return s, strconv.FormatInt(ts.Unix(), 10), nonce
}

// TestServiceAuthEd25519_Phase1_AcceptsSignedAndUnsigned — RFC § Phase 1
// (accept-both). For 2 weeks, requests WITHOUT the signature headers
// MUST still be accepted (the cloud proxy hasn't rolled out signing to
// every replica yet); requests WITH valid signatures get extra defense.
//
// Phase 2 + 3 flip the policy to require signed; that flag is gated by
// Config.RequireSignature. Phase 1 is the default in this test.
func TestServiceAuthEd25519_Phase1_AcceptsUnsigned(t *testing.T) {
	store := &stubMembershipStore{memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}}}
	authn := NewAuthenticator(Config{ServiceSecret: "shared-secret"})
	handler := authn.MiddlewareWithTTL(&stubValidator{}, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req.Header.Set("X-Service-Auth", "shared-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-svc")
	// NO X-LoopZero-Signature — Phase 1 must accept

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Phase 1 accept-both: unsigned request must succeed, got %d", rec.Code)
	}
}

func TestServiceAuthEd25519_Phase1_AcceptsValidSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	store := &stubMembershipStore{memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}}}
	authn := NewAuthenticator(Config{ServiceSecret: "shared-secret", Ed25519PublicKeys: []ed25519.PublicKey{pub}})
	handler := authn.MiddlewareWithTTL(&stubValidator{}, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	sig, tsStr, n := signedHeaders(t, priv, "svc-user", "tn-svc", "POST", "/api/v1/builds", time.Now(), "n1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req.Header.Set("X-Service-Auth", "shared-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-svc")
	req.Header.Set("X-LoopZero-Signature", sig)
	req.Header.Set("X-LoopZero-Timestamp", tsStr)
	req.Header.Set("X-LoopZero-Nonce", n)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("valid signature must succeed, got %d", rec.Code)
	}
}

// TestServiceAuthEd25519_RejectsTamperedSignature — even in Phase 1
// (accept-both), if the signature IS present and INVALID, the request
// is rejected. Phase 1 doesn't mean "accept any junk" — it means
// "accept missing signature for now". A garbled signature is a strong
// attack signal.
func TestServiceAuthEd25519_RejectsTamperedSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	store := &stubMembershipStore{memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}}}
	authn := NewAuthenticator(Config{ServiceSecret: "shared-secret", Ed25519PublicKeys: []ed25519.PublicKey{pub}})
	handler := authn.MiddlewareWithTTL(&stubValidator{}, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Sign for /api/v1/builds, then attempt to replay against /api/v1/billing.
	sig, tsStr, n := signedHeaders(t, priv, "svc-user", "tn-svc", "POST", "/api/v1/builds", time.Now(), "n1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing", nil)
	req.Header.Set("X-Service-Auth", "shared-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-svc")
	req.Header.Set("X-LoopZero-Signature", sig)
	req.Header.Set("X-LoopZero-Timestamp", tsStr)
	req.Header.Set("X-LoopZero-Nonce", n)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay across route must be rejected, got %d", rec.Code)
	}
}

// TestServiceAuthEd25519_RejectsStaleTimestamp — outside ±60s window,
// the request is rejected even with a correct signature. Tests the
// timestamp anti-replay path independently of the nonce path.
func TestServiceAuthEd25519_RejectsStaleTimestamp(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	store := &stubMembershipStore{memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}}}
	authn := NewAuthenticator(Config{ServiceSecret: "shared-secret", Ed25519PublicKeys: []ed25519.PublicKey{pub}})
	handler := authn.MiddlewareWithTTL(&stubValidator{}, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Sign with a timestamp 2 minutes ago (clearly outside the 60s window).
	stale := time.Now().Add(-2 * time.Minute)
	sig, tsStr, n := signedHeaders(t, priv, "svc-user", "tn-svc", "POST", "/api/v1/builds", stale, "n1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req.Header.Set("X-Service-Auth", "shared-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-svc")
	req.Header.Set("X-LoopZero-Signature", sig)
	req.Header.Set("X-LoopZero-Timestamp", tsStr)
	req.Header.Set("X-LoopZero-Nonce", n)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale timestamp must be rejected, got %d", rec.Code)
	}
}

// TestServiceAuthEd25519_RejectsReplayedNonce — the second use of the
// same (user_id, nonce) within the TTL is rejected. Tests the nonce
// store integration end-to-end through the middleware.
func TestServiceAuthEd25519_RejectsReplayedNonce(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	store := &stubMembershipStore{memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}}}
	authn := NewAuthenticator(Config{ServiceSecret: "shared-secret", Ed25519PublicKeys: []ed25519.PublicKey{pub}})
	handler := authn.MiddlewareWithTTL(&stubValidator{}, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	now := time.Now()
	sig, tsStr, n := signedHeaders(t, priv, "svc-user", "tn-svc", "POST", "/api/v1/builds", now, "fixed-nonce")

	// First request: accepted.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req.Header.Set("X-Service-Auth", "shared-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-svc")
	req.Header.Set("X-LoopZero-Signature", sig)
	req.Header.Set("X-LoopZero-Timestamp", tsStr)
	req.Header.Set("X-LoopZero-Nonce", n)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request must succeed, got %d", rec.Code)
	}

	// Second request with identical nonce: rejected.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req2.Header.Set("X-Service-Auth", "shared-secret")
	req2.Header.Set("X-LoopZero-User-ID", "svc-user")
	req2.Header.Set("X-LoopZero-Tenant", "tn-svc")
	req2.Header.Set("X-LoopZero-Signature", sig)
	req2.Header.Set("X-LoopZero-Timestamp", tsStr)
	req2.Header.Set("X-LoopZero-Nonce", n)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("nonce reuse must be rejected, got %d", rec2.Code)
	}
}

// TestServiceAuthEd25519_Phase2_RejectsUnsignedWhenFlagSet — RFC §
// Phase 2 (require-signed). After the operator sets
// Config.RequireSignature=true, any request WITHOUT signature
// headers is rejected with 401 even if the legacy shared secret matches.
func TestServiceAuthEd25519_Phase2_RejectsUnsignedWhenFlagSet(t *testing.T) {
	store := &stubMembershipStore{memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}}}
	authn := NewAuthenticator(Config{ServiceSecret: "shared-secret", RequireSignature: true})
	handler := authn.MiddlewareWithTTL(&stubValidator{}, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req.Header.Set("X-Service-Auth", "shared-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-svc")
	// NO X-LoopZero-Signature — Phase 2 must reject

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Phase 2 require-signed: unsigned request must be rejected, got %d", rec.Code)
	}
}
