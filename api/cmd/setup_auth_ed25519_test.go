// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"os"
	"testing"
)

// TestLoadServiceAuthEd25519Trust_PopulatesCurrentOnly — happy path with
// only LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT set (steady-state, no
// rotation in flight). The trust set ends with exactly one public key.
func TestLoadServiceAuthEd25519Trust_PopulatesCurrentOnly(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT", base64.StdEncoding.EncodeToString(pub))
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS", "")

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	trust := loadServiceAuthEd25519Trust(logger)

	if got := len(trust); got != 1 {
		t.Fatalf("want 1 trusted key (current only), got %d", got)
	}
}

// TestLoadServiceAuthEd25519Trust_PopulatesCurrentAndPrevious — rotation
// window: BOTH current and previous public keys must be in the trust
// set so signatures from EITHER key validate.
func TestLoadServiceAuthEd25519Trust_PopulatesCurrentAndPrevious(t *testing.T) {
	curPub, _, _ := ed25519.GenerateKey(rand.Reader)
	prevPub, _, _ := ed25519.GenerateKey(rand.Reader)
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT", base64.StdEncoding.EncodeToString(curPub))
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS", base64.StdEncoding.EncodeToString(prevPub))

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	trust := loadServiceAuthEd25519Trust(logger)

	if got := len(trust); got != 2 {
		t.Fatalf("want 2 trusted keys (current + previous), got %d", got)
	}
}

// TestLoadServiceAuthEd25519Trust_EmptyEnv_LeavesTrustSetEmpty — Phase
// 1 default with no key configured. Trust set stays empty; middleware's
// fail-closed branch (ErrNoTrustedKeys) is exercised only when a signed
// request actually arrives (Phase 1 accept-both ignores unsigned).
func TestLoadServiceAuthEd25519Trust_EmptyEnv_LeavesTrustSetEmpty(t *testing.T) {
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT", "")
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS", "")

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	trust := loadServiceAuthEd25519Trust(logger)

	if got := len(trust); got != 0 {
		t.Fatalf("want 0 trusted keys (Phase 1 unconfigured), got %d", got)
	}
}

// TestLoadServiceAuthEd25519Trust_MalformedCurrent_FailsClosed — per
// RFC Q5 fail-closed: a malformed env var MUST crash startup, not
// silently leave the trust set empty (which would silently disable
// dual-control without surfacing the operator error).
//
// We can't test os.Exit directly; instead the loader returns the
// error and the caller (setupAuth) wraps it into a fatal log.
// Test the loader function level.
func TestLoadServiceAuthEd25519Trust_MalformedCurrent_FailsClosed(t *testing.T) {
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT", "@@@not-valid-base64@@@")
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS", "")

	if _, err := loadServiceAuthEd25519TrustE(); err == nil {
		t.Fatal("malformed current key must produce an error (fail-closed)")
	}
}

// TestLoadServiceAuthEd25519Trust_WrongKeySize_FailsClosed — base64
// decodes but the resulting bytes are not ed25519.PublicKeySize.
// Same fail-closed requirement as malformed base64.
func TestLoadServiceAuthEd25519Trust_WrongKeySize_FailsClosed(t *testing.T) {
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT", base64.StdEncoding.EncodeToString([]byte("too-short")))
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS", "")

	if _, err := loadServiceAuthEd25519TrustE(); err == nil {
		t.Fatal("wrong-size current key must produce an error (fail-closed)")
	}
}

// TestServiceAuthRequireSignatureFlag_DefaultFalse — Phase 1 default.
// Without the env var set, the require-signature flag stays false
// (accept-both). Operator flips to true only after the 7-day soak per
// Q3 cutover decision.
func TestServiceAuthRequireSignatureFlag_DefaultFalse(t *testing.T) {
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH_REQUIRE_SIGNATURE", "")

	if serviceAuthRequireSignature() {
		t.Fatal("unset env var must default to false (Phase 1 accept-both)")
	}
}

// TestServiceAuthRequireSignatureFlag_TrueValue — explicit operator
// opt-in. Accepts "true" / "1" / "yes" so the operator runbook can
// document a friendly value.
func TestServiceAuthRequireSignatureFlag_TrueValue(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "1", "yes", "YES"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("LOOPRUNTIME_SERVICE_AUTH_REQUIRE_SIGNATURE", v)
			if !serviceAuthRequireSignature() {
				t.Errorf("env=%q must enable Phase 2 require-signed", v)
			}
		})
	}
}
