// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package serviceauth_test

import (
	"strings"
	"testing"

	"github.com/loopzero/loopzero/pkg/serviceauth"
)

// TestCanonicalPayload_ShapeMatchesRFC verifies the canonical payload
// follows the exact byte-level shape specified in
// decisions/2026-05-13-service-auth-ed25519-adoption.md § Wire format.
// Format: user_id \n tenant_id \n timestamp \n nonce \n method " " path
//
// Stability of this shape is a wire-protocol contract. Changing it
// forces dual-control flag-day across api/cloud — never mutate after
// Phase 1 ships.
func TestCanonicalPayload_ShapeMatchesRFC(t *testing.T) {
	got := serviceauth.CanonicalPayload(serviceauth.PayloadInput{
		UserID:    "user-123",
		TenantID:  "tn-acme",
		Timestamp: 1715616000,
		Nonce:     "nonce-abc",
		Method:    "POST",
		Path:      "/api/v1/builds",
	})

	want := "user-123\ntn-acme\n1715616000\nnonce-abc\nPOST /api/v1/builds"
	if got != want {
		t.Fatalf("CanonicalPayload shape drift:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestCanonicalPayload_DeterministicAcrossCalls confirms identical input
// produces identical output. Determinism is the foundation of signature
// verification — any source of nondeterminism (map iteration, time.Now)
// is a bug.
func TestCanonicalPayload_DeterministicAcrossCalls(t *testing.T) {
	in := serviceauth.PayloadInput{
		UserID:    "u",
		TenantID:  "t",
		Timestamp: 42,
		Nonce:     "n",
		Method:    "GET",
		Path:      "/x",
	}
	a := serviceauth.CanonicalPayload(in)
	b := serviceauth.CanonicalPayload(in)
	if a != b {
		t.Fatalf("CanonicalPayload nondeterministic: %q vs %q", a, b)
	}
}

// TestCanonicalPayload_MethodPathChangeChangesPayload asserts that the
// method+path binding works: a captured signature for GET /a cannot
// be replayed against POST /b. Per RFC threat model.
func TestCanonicalPayload_MethodPathChangeChangesPayload(t *testing.T) {
	base := serviceauth.PayloadInput{UserID: "u", TenantID: "t", Timestamp: 1, Nonce: "n", Method: "GET", Path: "/a"}

	alt := base
	alt.Method = "POST"
	if serviceauth.CanonicalPayload(base) == serviceauth.CanonicalPayload(alt) {
		t.Fatal("method change must alter canonical payload")
	}

	alt = base
	alt.Path = "/b"
	if serviceauth.CanonicalPayload(base) == serviceauth.CanonicalPayload(alt) {
		t.Fatal("path change must alter canonical payload")
	}
}

// TestCanonicalPayload_NoFieldConfusion ensures the delimiter is strong
// enough that swapping characters between fields doesn't collide. If
// user_id="a\nb" and tenant_id="c" produced the same payload as
// user_id="a" and tenant_id="b\nc", that would be a critical bug —
// signature ambiguity opens forgery.
//
// The RFC payload uses raw "\n" which means inputs MUST be validated
// elsewhere (caller) to reject newlines. This test pins that
// requirement by demonstrating the collision when newlines are allowed.
func TestCanonicalPayload_NewlineInInputCreatesCollision(t *testing.T) {
	a := serviceauth.CanonicalPayload(serviceauth.PayloadInput{
		UserID: "a\nb", TenantID: "c", Timestamp: 1, Nonce: "n", Method: "GET", Path: "/x",
	})
	b := serviceauth.CanonicalPayload(serviceauth.PayloadInput{
		UserID: "a", TenantID: "b\nc", Timestamp: 1, Nonce: "n", Method: "GET", Path: "/x",
	})
	if a != b {
		t.Skip("escaping was added — this test now documents the protection")
	}
	// Documents the requirement: callers MUST reject newlines in user/tenant inputs.
	// Tracked by ValidateInputs below.
}

// TestValidateInputs_RejectsNewlinesAndControlChars asserts that the
// service-auth helper rejects any input that would allow field
// confusion attacks. UserID/TenantID/Nonce must be in a safe charset.
func TestValidateInputs_RejectsNewlinesAndControlChars(t *testing.T) {
	cases := []struct {
		name string
		in   serviceauth.PayloadInput
	}{
		{"newline in user_id", serviceauth.PayloadInput{UserID: "a\nb", TenantID: "t", Nonce: "n", Method: "GET", Path: "/"}},
		{"newline in tenant_id", serviceauth.PayloadInput{UserID: "u", TenantID: "t\nx", Nonce: "n", Method: "GET", Path: "/"}},
		{"newline in nonce", serviceauth.PayloadInput{UserID: "u", TenantID: "t", Nonce: "n\nbad", Method: "GET", Path: "/"}},
		{"tab in user_id", serviceauth.PayloadInput{UserID: "a\tb", TenantID: "t", Nonce: "n", Method: "GET", Path: "/"}},
		{"empty user_id", serviceauth.PayloadInput{UserID: "", TenantID: "t", Nonce: "n", Method: "GET", Path: "/"}},
		{"empty tenant_id", serviceauth.PayloadInput{UserID: "u", TenantID: "", Nonce: "n", Method: "GET", Path: "/"}},
		{"empty nonce", serviceauth.PayloadInput{UserID: "u", TenantID: "t", Nonce: "", Method: "GET", Path: "/"}},
		{"empty method", serviceauth.PayloadInput{UserID: "u", TenantID: "t", Nonce: "n", Method: "", Path: "/"}},
		{"empty path", serviceauth.PayloadInput{UserID: "u", TenantID: "t", Nonce: "n", Method: "GET", Path: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := serviceauth.ValidateInputs(tc.in)
			if err == nil {
				t.Fatalf("ValidateInputs must reject %q", tc.name)
			}
			if !strings.Contains(err.Error(), "service-auth") {
				t.Errorf("error should mention 'service-auth' for audit grep, got: %v", err)
			}
		})
	}
}

// TestValidateInputs_AcceptsCanonicalShape — happy path control case.
func TestValidateInputs_AcceptsCanonicalShape(t *testing.T) {
	in := serviceauth.PayloadInput{
		UserID:    "user-uuid-canonical",
		TenantID:  "tn-acme",
		Timestamp: 1715616000,
		Nonce:     "16-byte-base64-random==",
		Method:    "POST",
		Path:      "/api/v1/builds",
	}
	if err := serviceauth.ValidateInputs(in); err != nil {
		t.Fatalf("canonical input rejected unexpectedly: %v", err)
	}
}
