// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T4.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md —
// property-based extension for SessionTokenIssuer + Validator
// adversarial inputs. Closes M04 LOW gap.

package jwt

import (
	"strings"
	"testing"
	"testing/quick"
	"time"
)

// TestProperty_SessionIssuerRejectsBlatantlyMalformed — for any
// random non-JWT string, ValidateSession MUST return an error
// (never panic, never return a Claims object).
func TestProperty_SessionIssuerRejectsBlatantlyMalformed(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret", "test-iss", "test-aud", time.Hour)
	f := func(garbage string) bool {
		// Skip strings that happen to look like valid JWT structure (three dots
		// + base64) — those are tested via TestProperty_SessionIssuerRejectsTamperedSignature.
		if strings.Count(garbage, ".") == 2 && len(garbage) > 20 {
			return true
		}
		_, err := issuer.ValidateSession(garbage)
		return err != nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("ValidateSession must reject malformed input: %v", err)
	}
}

// TestProperty_SessionIssuerRejectsEmptyToken — empty string ALWAYS
// fails validation. Catches regression where empty checks are
// accidentally removed.
func TestProperty_SessionIssuerRejectsEmptyToken(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret", "test-iss", "test-aud", time.Hour)
	for i := 0; i < 100; i++ {
		_, err := issuer.ValidateSession("")
		if err == nil {
			t.Fatal("empty token must always fail validation")
		}
	}
}

// TestProperty_SessionIssuerRejectsTamperedSignature — issue a real
// token, then flip random bytes anywhere. Validation MUST fail.
func TestProperty_SessionIssuerRejectsTamperedSignature(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret", "test-iss", "test-aud", time.Hour)
	token, _, err := issuer.Issue("user-1", "test@example.com", "Test User", "tenant-1")
	if err != nil {
		t.Fatalf("setup: Issue failed: %v", err)
	}

	f := func(offset uint16, flipByte byte) bool {
		if len(token) == 0 {
			return true
		}
		idx := int(offset) % len(token)
		buf := []byte(token)
		original := buf[idx]
		buf[idx] = flipByte ^ original // XOR-flip ensures actual mutation
		if buf[idx] == original {
			return true // no-op flip (flipByte==0 against this index)
		}
		_, valErr := issuer.ValidateSession(string(buf))
		return valErr != nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatalf("tampered token must fail validation: %v", err)
	}
}

// TestProperty_SessionIssuerWrongSecretFails — token issued by secret
// A cannot be validated by secret B. Catches regression where the
// secret is hashed or otherwise normalized in a way that collides.
func TestProperty_SessionIssuerWrongSecretFails(t *testing.T) {
	a := NewSessionTokenIssuer("secret-A-fixed-known-value-xyz", "iss", "aud", time.Hour)
	token, _, err := a.Issue("user-1", "test@example.com", "Test User", "tenant-1")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	f := func(otherSecret string) bool {
		if otherSecret == "secret-A-fixed-known-value-xyz" {
			return true // same secret — would validate
		}
		if otherSecret == "" {
			return true // empty secret skip
		}
		b := NewSessionTokenIssuer(otherSecret, "iss", "aud", time.Hour)
		_, valErr := b.ValidateSession(token)
		return valErr != nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatalf("token issued by A must fail validation by other secrets: %v", err)
	}
}

// TestProperty_IssuerWithExpiredLifetimeRejected — tokens issued
// with negative lifetime are immediately invalid (per JWT exp claim).
func TestProperty_IssuerWithExpiredLifetimeRejected(t *testing.T) {
	// Issue with extreme negative lifetime to guarantee exp in past
	issuer := NewSessionTokenIssuer("test-secret", "iss", "aud", -10*time.Minute)
	token, _, err := issuer.Issue("user-1", "test@example.com", "Test User", "tenant-1")
	if err != nil {
		t.Fatalf("issue must succeed even with negative lifetime: %v", err)
	}
	_, valErr := issuer.ValidateSession(token)
	if valErr == nil {
		t.Fatal("expired token must fail validation")
	}
}
