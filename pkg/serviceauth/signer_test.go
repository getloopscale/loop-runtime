// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package serviceauth_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/pkg/serviceauth"
)

// TestSign_VerifyRoundTrip — happy path: a signature produced by Sign
// validates with Verify against the matching public key.
func TestSign_VerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key gen: %v", err)
	}

	in := serviceauth.PayloadInput{
		UserID: "u", TenantID: "t", Timestamp: 1715616000, Nonce: "n1",
		Method: "POST", Path: "/api/v1/builds",
	}

	sig, err := serviceauth.Sign(priv, in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if sig == "" {
		t.Fatal("signature must be non-empty")
	}

	if err := serviceauth.Verify([]ed25519.PublicKey{pub}, sig, in); err != nil {
		t.Fatalf("Verify of own signature failed: %v", err)
	}
}

// TestVerify_RejectsTamperedPayload — the signature is bound to the
// canonical payload. Changing any field breaks verification.
func TestVerify_RejectsTamperedPayload(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	in := serviceauth.PayloadInput{
		UserID: "u", TenantID: "t", Timestamp: 1715616000, Nonce: "n1",
		Method: "GET", Path: "/api/v1/billing",
	}
	sig, err := serviceauth.Sign(priv, in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	cases := []struct {
		name  string
		mutFn func(*serviceauth.PayloadInput)
	}{
		{"user_id", func(p *serviceauth.PayloadInput) { p.UserID = "attacker" }},
		{"tenant_id", func(p *serviceauth.PayloadInput) { p.TenantID = "victim" }},
		{"timestamp", func(p *serviceauth.PayloadInput) { p.Timestamp = 9999 }},
		{"nonce", func(p *serviceauth.PayloadInput) { p.Nonce = "replay" }},
		{"method-binding (RFC: replay GET sig on POST blocked)", func(p *serviceauth.PayloadInput) { p.Method = "POST" }},
		{"path-binding (RFC: replay /billing sig on /builds blocked)", func(p *serviceauth.PayloadInput) { p.Path = "/api/v1/builds" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := in
			tc.mutFn(&tampered)
			err := serviceauth.Verify([]ed25519.PublicKey{pub}, sig, tampered)
			if err == nil {
				t.Fatalf("tampered (%s) signature must fail verify", tc.name)
			}
			if !errors.Is(err, serviceauth.ErrSignatureInvalid) {
				t.Errorf("tampered (%s): wrong error class, want ErrSignatureInvalid, got %v", tc.name, err)
			}
		})
	}
}

// TestVerify_DualKeyAcceptance — during rotation window, signatures
// from both current AND previous private keys must validate. Same
// pattern as pkg/jwt/validator.go::WithPreviousSecret (cited in RFC).
func TestVerify_DualKeyAcceptance(t *testing.T) {
	pubCur, privCur, _ := ed25519.GenerateKey(rand.Reader)
	pubPrev, privPrev, _ := ed25519.GenerateKey(rand.Reader)

	in := serviceauth.PayloadInput{
		UserID: "u", TenantID: "t", Timestamp: 1, Nonce: "n",
		Method: "GET", Path: "/x",
	}

	sigCur, _ := serviceauth.Sign(privCur, in)
	sigPrev, _ := serviceauth.Sign(privPrev, in)

	// Both signatures valid against the trust set {cur, prev}.
	keys := []ed25519.PublicKey{pubCur, pubPrev}
	if err := serviceauth.Verify(keys, sigCur, in); err != nil {
		t.Errorf("current-key signature rejected: %v", err)
	}
	if err := serviceauth.Verify(keys, sigPrev, in); err != nil {
		t.Errorf("previous-key signature rejected during rotation window: %v", err)
	}

	// After rotation completes, drop previous from trust set.
	curOnly := []ed25519.PublicKey{pubCur}
	if err := serviceauth.Verify(curOnly, sigPrev, in); err == nil {
		t.Error("previous-key signature must be rejected once previous is dropped from trust set")
	}
}

// TestVerify_RejectsEmptyTrustSet — defense in depth: if the public
// key cannot be loaded (ESO sync failure, ConfigMap missing), Verify
// fails closed. RFC Q5 decision: fail-closed for public-key fetch.
func TestVerify_RejectsEmptyTrustSet(t *testing.T) {
	in := serviceauth.PayloadInput{
		UserID: "u", TenantID: "t", Timestamp: 1, Nonce: "n",
		Method: "GET", Path: "/x",
	}
	err := serviceauth.Verify(nil, "any-sig", in)
	if !errors.Is(err, serviceauth.ErrNoTrustedKeys) {
		t.Fatalf("empty trust set must return ErrNoTrustedKeys, got %v", err)
	}
}

// TestVerify_RejectsMalformedSignatureBase64 — defense against bogus
// header value (truncation, corruption, manual injection of garbage).
func TestVerify_RejectsMalformedSignatureBase64(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	in := serviceauth.PayloadInput{
		UserID: "u", TenantID: "t", Timestamp: 1, Nonce: "n",
		Method: "GET", Path: "/x",
	}
	err := serviceauth.Verify([]ed25519.PublicKey{pub}, "@@@not-base64@@@", in)
	if err == nil {
		t.Fatal("malformed base64 must be rejected")
	}
	if !strings.Contains(err.Error(), "service-auth") {
		t.Errorf("error should mention 'service-auth' for audit grep, got %v", err)
	}
}

// TestSign_RejectsInvalidInput — Sign must call ValidateInputs and
// reject newline-injection attempts before producing a signature.
// Without this guard, an attacker who controls UserID input could
// craft a payload that collides with a different valid payload.
func TestSign_RejectsInvalidInput(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, err := serviceauth.Sign(priv, serviceauth.PayloadInput{
		UserID: "a\nb", TenantID: "t", Timestamp: 1, Nonce: "n",
		Method: "GET", Path: "/x",
	})
	if err == nil {
		t.Fatal("Sign must reject inputs that fail ValidateInputs")
	}
}
