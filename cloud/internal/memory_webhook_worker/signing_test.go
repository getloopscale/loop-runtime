// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package memory_webhook_worker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// T5.5 — known-fixture vector. The HMAC is computed manually from the
// (secret, timestamp, body) tuple so a Stripe-compatible consumer can
// re-derive it independently.
func TestSign_KnownFixture(t *testing.T) {
	secret := "REDACTED_SECRET_013"
	ts := int64(1717678800) // 2026-06-06 13:00:00 UTC
	body := []byte(`{"event":"memory.forgotten","memory_id":"m1"}`)

	got := Sign(secret, ts, body)

	// Recompute the expected MAC the way a consumer SDK would.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("1717678800."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Errorf("Sign() = %q, want %q", got, want)
	}
	// Hex SHA-256 is always 64 chars lowercase.
	if len(got) != 64 {
		t.Errorf("hex length = %d, want 64", len(got))
	}
	if strings.ToLower(got) != got {
		t.Errorf("expected lowercase hex, got %q", got)
	}
}

// T5.5 — different bodies produce different signatures (sanity that the
// MAC actually covers the body, not just the timestamp).
func TestSign_DifferentBodiesDifferentSigs(t *testing.T) {
	secret := "whsec_x"
	ts := int64(1)
	s1 := Sign(secret, ts, []byte("a"))
	s2 := Sign(secret, ts, []byte("b"))
	if s1 == s2 {
		t.Fatal("different bodies must produce different signatures — MAC is not body-bound")
	}
}

// T5.5 — different secrets produce different signatures (sanity that the
// MAC covers the key, not just the body).
func TestSign_DifferentSecretsDifferentSigs(t *testing.T) {
	body := []byte("x")
	ts := int64(1)
	s1 := Sign("whsec_a", ts, body)
	s2 := Sign("whsec_b", ts, body)
	if s1 == s2 {
		t.Fatal("different secrets must produce different signatures — MAC key is not bound")
	}
}

// T5.5 — different timestamps produce different signatures (replay-attack
// defense; consumer enforces a window).
func TestSign_DifferentTimestampsDifferentSigs(t *testing.T) {
	secret := "whsec_x"
	body := []byte("x")
	s1 := Sign(secret, 1, body)
	s2 := Sign(secret, 2, body)
	if s1 == s2 {
		t.Fatal("different timestamps must produce different signatures — replay defense broken")
	}
}

// T5.5 — HeaderValue matches Stripe's exact format `t={ts},v1={hex}`.
func TestHeaderValue_StripeFormat(t *testing.T) {
	got := HeaderValue(1717678800, "abc123")
	want := "t=1717678800,v1=abc123"
	if got != want {
		t.Errorf("HeaderValue() = %q, want %q", got, want)
	}
}
