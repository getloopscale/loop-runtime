// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/pkg/serviceauth"
)

// TestServiceSigner_AttachAddsAllThreeHeaders — happy path: a configured
// signer adds X-LoopZero-Signature, X-LoopZero-Timestamp, X-LoopZero-Nonce to the
// outbound request. Engine middleware then runs verifyServiceAuthSignature
// against the canonical payload built from these headers.
func TestServiceSigner_AttachAddsAllThreeHeaders(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := auth.NewServiceSigner(priv)
	if err != nil {
		t.Fatalf("NewServiceSigner: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://engine/api/v1/builds", nil)
	req.Header.Set("X-LoopZero-User-ID", "u")
	req.Header.Set("X-LoopZero-Tenant", "t")

	if err := signer.Attach(req); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if req.Header.Get("X-LoopZero-Signature") == "" {
		t.Error("X-LoopZero-Signature missing")
	}
	if req.Header.Get("X-LoopZero-Timestamp") == "" {
		t.Error("X-LoopZero-Timestamp missing")
	}
	if req.Header.Get("X-LoopZero-Nonce") == "" {
		t.Error("X-LoopZero-Nonce missing")
	}
}

// TestServiceSigner_ProducesEngineVerifiableSignature — the integration
// invariant. The signature emitted by the cloud signer MUST validate
// against the matching public key when fed through the engine's
// pkg/serviceauth.Verify call.
func TestServiceSigner_ProducesEngineVerifiableSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := auth.NewServiceSigner(priv)

	req := httptest.NewRequest(http.MethodPost, "http://engine/api/v1/builds", nil)
	req.Header.Set("X-LoopZero-User-ID", "user-canonical")
	req.Header.Set("X-LoopZero-Tenant", "tn-canonical")

	if err := signer.Attach(req); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	// Reconstruct the canonical payload exactly as the engine would.
	tsInt, err := strconv.ParseInt(req.Header.Get("X-LoopZero-Timestamp"), 10, 64)
	if err != nil {
		t.Fatalf("timestamp parse: %v", err)
	}
	in := serviceauth.PayloadInput{
		UserID:    req.Header.Get("X-LoopZero-User-ID"),
		TenantID:  req.Header.Get("X-LoopZero-Tenant"),
		Timestamp: tsInt,
		Nonce:     req.Header.Get("X-LoopZero-Nonce"),
		Method:    req.Method,
		Path:      req.URL.Path,
	}
	if err := serviceauth.Verify([]ed25519.PublicKey{pub}, req.Header.Get("X-LoopZero-Signature"), in); err != nil {
		t.Fatalf("signature did not round-trip: %v", err)
	}
}

// TestServiceSigner_NonceIsUniquePerCall — replay protection on the
// proxy side is opportunistic but real. Two consecutive calls must
// emit distinct nonces. Without this, the engine's nonce store
// rejects the second call within the same TTL window even though it
// is a legitimate fresh request.
func TestServiceSigner_NonceIsUniquePerCall(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := auth.NewServiceSigner(priv)

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://e/x", nil)
		req.Header.Set("X-LoopZero-User-ID", "u")
		req.Header.Set("X-LoopZero-Tenant", "t")
		if err := signer.Attach(req); err != nil {
			t.Fatalf("iter %d Attach: %v", i, err)
		}
		n := req.Header.Get("X-LoopZero-Nonce")
		if seen[n] {
			t.Fatalf("nonce collision on iter %d: %s", i, n)
		}
		seen[n] = true
	}
}

// TestServiceSigner_RejectsMissingIdentityHeaders — Attach refuses to
// produce a signature when the upstream user-identity headers are
// absent. Without this guard, the proxy could silently sign over
// empty identity (would fail engine's ValidateInputs but the failure
// would surface in audit_log rather than in cloud logs, hiding the
// bug from operators).
func TestServiceSigner_RejectsMissingIdentityHeaders(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := auth.NewServiceSigner(priv)

	cases := []struct {
		name      string
		setHeader func(req *http.Request)
	}{
		{"no user-id", func(r *http.Request) { r.Header.Set("X-LoopZero-Tenant", "t") }},
		{"no tenant", func(r *http.Request) { r.Header.Set("X-LoopZero-User-ID", "u") }},
		{"neither", func(r *http.Request) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://e/x", nil)
			tc.setHeader(req)
			err := signer.Attach(req)
			if err == nil {
				t.Fatal("Attach must refuse without identity headers")
			}
		})
	}
}

// TestNewServiceSigner_AcceptsBase64EncodedKey — convenience constructor
// for the operator runbook: the env var carries a base64 string (per
// RFC § Key management). NewServiceSignerFromBase64 decodes + validates
// + wires into NewServiceSigner.
func TestNewServiceSigner_AcceptsBase64EncodedKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	encoded := base64.StdEncoding.EncodeToString(priv)

	signer, err := auth.NewServiceSignerFromBase64(encoded)
	if err != nil {
		t.Fatalf("NewServiceSignerFromBase64: %v", err)
	}
	if signer == nil {
		t.Fatal("signer must not be nil")
	}
}

// TestNewServiceSigner_RejectsMalformedKey — operator runbook safety:
// an env-var typo or accidental truncation must fail loudly at startup
// rather than silently producing a broken signer.
func TestNewServiceSigner_RejectsMalformedKey(t *testing.T) {
	cases := []string{
		"",
		"@@@not-base64@@@",
		base64.StdEncoding.EncodeToString([]byte("too-short")),
	}
	for _, s := range cases {
		_, err := auth.NewServiceSignerFromBase64(s)
		if err == nil {
			t.Errorf("NewServiceSignerFromBase64(%q) must fail", s)
		}
	}
}

// TestServiceSigner_TimestampIsRecent — sanity check that the signer
// uses time.Now() (not a fixed value) for the timestamp. Without this
// every request would carry a stale ts and the engine would reject
// after the first window passes.
func TestServiceSigner_TimestampIsRecent(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := auth.NewServiceSigner(priv)

	before := time.Now().Unix()
	req := httptest.NewRequest(http.MethodPost, "http://e/x", nil)
	req.Header.Set("X-LoopZero-User-ID", "u")
	req.Header.Set("X-LoopZero-Tenant", "t")
	if err := signer.Attach(req); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	after := time.Now().Unix()

	tsStr := req.Header.Get("X-LoopZero-Timestamp")
	tsInt, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		t.Fatalf("ts parse: %v", err)
	}
	if tsInt < before || tsInt > after {
		t.Errorf("timestamp %d outside [%d, %d]", tsInt, before, after)
	}
}
