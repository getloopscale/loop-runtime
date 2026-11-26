// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package naming

import (
	"testing"
	"testing/quick"
)

// TestProperty_SanitizeAlwaysProducesValidOrEmpty verifies the
// fundamental invariant of Sanitize: for ANY input, the output is
// either empty OR a valid RFC 1123 DNS label.
//
// This is the most important naming invariant — if a controller
// receives a tenant name like "🦄user@evil" and Sanitize produces
// something that ValidateKubernetesName rejects, the controller
// crashes mid-reconcile. Per ADR-0035 + gate PKG-G18.
func TestProperty_SanitizeAlwaysProducesValidOrEmpty(t *testing.T) {
	f := func(s string) bool {
		out := Sanitize(s)
		if out == "" {
			return true
		}
		return ValidateKubernetesName(out) == nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 10000}); err != nil {
		t.Fatalf("Sanitize invariant failed: %v", err)
	}
}

// TestProperty_ValidateKubernetesNameIsDeterministic verifies that
// validation is a pure function — same input always yields same
// answer. Catches regressions where validation is accidentally made
// stateful (caching, per-call regex compile races, etc.).
func TestProperty_ValidateKubernetesNameIsDeterministic(t *testing.T) {
	f := func(s string) bool {
		first := ValidateKubernetesName(s)
		for i := 0; i < 50; i++ {
			second := ValidateKubernetesName(s)
			// Compare just nil-ness; error messages are stable.
			if (first == nil) != (second == nil) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("ValidateKubernetesName determinism failed: %v", err)
	}
}

// TestProperty_SanitizeIsIdempotent verifies that Sanitize(Sanitize(s))
// equals Sanitize(s) for every input. If Sanitize ever produces an
// output that itself needs further sanitization, downstream code may
// double-process and produce a different name.
func TestProperty_SanitizeIsIdempotent(t *testing.T) {
	f := func(s string) bool {
		once := Sanitize(s)
		twice := Sanitize(once)
		return once == twice
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("Sanitize idempotency failed: %v", err)
	}
}

// TestProperty_SanitizeRespectsMaxLength verifies that Sanitize never
// returns a name longer than the K8s max (63 characters). Truncation
// is the responsibility of Sanitize, not the caller.
func TestProperty_SanitizeRespectsMaxLength(t *testing.T) {
	f := func(s string) bool {
		out := Sanitize(s)
		return len(out) <= MaxKubernetesNameLength
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("Sanitize length invariant failed: %v", err)
	}
}
