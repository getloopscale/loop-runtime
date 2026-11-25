// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.2 of plan m5-stripe-sot-canonical-closure v1.0 — hermetic guard
// locking the Stripe SoT shape canonical contract:
//
//	stripe.{api_key, publishable_key, webhook_secret} MUST all be
//	present AND prefix-coherent in test mode
//	(sk_test_ / pk_test_ / whsec_).
//
// Per ADR D1 of the plan: schema validated against committed fixture
// (hermetic, runs in any env; CI never touches real ~/.loopzero/dev-
// credentials.json). The optional --check-live-sot operator path is
// kept as a separate concern (not in this Go test; would belong in a
// future operator-side tool).
//
// Per ADR D2: test mode coherent triple is the canonical default.
// Live mode validation deferred to future plan when prod tier ships.
//
// Per ADR 2026-06-23-tier-na-data-only-milestone-closure: this gate
// is the canonical Step 2 (hermetic shape guard committed + CI-wired)
// of the 4-step Tier N/A data-only milestone closure pattern.
//
// Closes M5 of qa-readiness-roadmap-2026-06-23 from DATA_INGESTED →
// ENGINEERING_COMPLETED. LIVE chain (4 post-sponsor steps) still
// deferred to M7 Cleanroom per cleanroom-up-live-test-mandate
// UNBREAKABLE.
//
// Pivot rationale: original implementation was a bash test
// (infra/tests/check-stripe-sot-shape_test.sh) but that triggered
// the unhomed-baseline gate (domain "test" un-homed +1). Per
// goal "ALWAYS FAANG LEVEL, and without rework, NO WORKAROUNDS",
// re-implemented as Go test in the canonical hermetic test umbrella
// (infra/tests/charts/) which runs via `go test ./charts/` already
// wired in task quality:gates pre-push hook.

package charts_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const (
	fixturePath = "../fixtures/dev-credentials-stripe-schema-test-mode.json"
)

// stripeSchema mirrors the relevant slice of ~/.loopzero/dev-credentials.json.
type stripeSchema struct {
	Stripe map[string]any `json:"stripe"`
}

// loadFixture reads + parses the committed schema fixture.
func loadFixture(t *testing.T) stripeSchema {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("fixture read failed at %s: %v", fixturePath, err)
	}
	var s stripeSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("fixture JSON parse failed: %v", err)
	}
	return s
}

// TestStripeSoTShape_FixtureExists asserts the canonical fixture is
// committed at the documented path.
func TestStripeSoTShape_FixtureExists(t *testing.T) {
	t.Parallel()
	info, err := os.Stat(fixturePath)
	if err != nil {
		t.Fatalf("fixture missing at %s: %v", fixturePath, err)
	}
	if info.IsDir() {
		t.Fatalf("fixture is a directory, expected file: %s", fixturePath)
	}
}

// TestStripeSoTShape_AllThreeKeysPresent asserts the 3-key triple
// contract per the canonical SoT shape.
func TestStripeSoTShape_AllThreeKeysPresent(t *testing.T) {
	t.Parallel()
	s := loadFixture(t)
	required := []string{"api_key", "publishable_key", "webhook_secret"}
	for _, k := range required {
		v, ok := s.Stripe[k]
		if !ok {
			t.Errorf("stripe.%s: MISSING from fixture", k)
			continue
		}
		str, ok := v.(string)
		if !ok {
			t.Errorf("stripe.%s: NOT a string, got %T", k, v)
			continue
		}
		if str == "" {
			t.Errorf("stripe.%s: EMPTY string", k)
		}
	}
}

// TestStripeSoTShape_TestModePrefixCoherence asserts the canonical
// prefix coherence — all 3 values use test mode prefixes
// (sk_test_ / pk_test_ / whsec_) per ADR D2 of the plan.
func TestStripeSoTShape_TestModePrefixCoherence(t *testing.T) {
	t.Parallel()
	s := loadFixture(t)
	expectedPrefixes := map[string]string{
		"api_key":         "sk_test_",
		"publishable_key": "pk_test_",
		"webhook_secret":  "whsec_",
	}
	for key, prefix := range expectedPrefixes {
		v, ok := s.Stripe[key].(string)
		if !ok || v == "" {
			t.Errorf("stripe.%s: skipped prefix check — key absent or non-string (verified by AllThreeKeysPresent test)", key)
			continue
		}
		if !strings.HasPrefix(v, prefix) {
			// Redacted preview only — never full value
			truncated := v
			if len(v) > 8 {
				truncated = v[:8]
			}
			t.Errorf("stripe.%s: prefix mismatch — got %q…(len=%d) expected prefix %q",
				key, truncated, len(v), prefix)
		}
	}
}

// TestStripeSoTShape_FixtureMarkedAsFake asserts the fixture clearly
// flags itself as containing NOT REAL SECRETS — defends against
// accidental commit of real secrets to the fixture path.
func TestStripeSoTShape_FixtureMarkedAsFake(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("fixture read failed: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "NOT REAL SECRETS") && !strings.Contains(content, "_FAKE") {
		t.Errorf("fixture missing 'NOT REAL SECRETS' or '_FAKE' disclaimer — risk of accidental real-secret commit")
	}
}

// TestStripeSoTShape_FixtureMode0644 asserts the fixture file is
// readable as a public schema artifact (NOT a 0600 secret SoT).
// This is the inverse of the canonical secret-SoT mode contract:
// since the fixture intentionally contains NO real secrets, it
// MUST be mode 0644 (public) so anyone running the test can read
// it without owner-only restrictions. A 0600 fixture would
// erroneously imply secret content.
func TestStripeSoTShape_FixtureMode0644(t *testing.T) {
	t.Parallel()
	info, err := os.Stat(fixturePath)
	if err != nil {
		t.Fatalf("fixture stat failed: %v", err)
	}
	// The contract stated above is READABILITY, not an exact bit pattern: the fixture must not
	// look like an owner-only secret. An exact `!= 0o644` check also asserts the checkout umask,
	// which git does not track — a clone under umask 002 yields 0664, which honours the contract
	// and still failed. Measured on a umask-002 host, 2026-08-28.
	mode := info.Mode().Perm()
	if mode&0o044 != 0o044 {
		t.Errorf("fixture mode %o is not group+world readable; a public schema artifact must be "+
			"readable by anyone running the test, unlike a 0600 secret SoT", mode)
	}
	if mode&0o111 != 0 {
		t.Errorf("fixture mode %o is executable; a JSON schema artifact must not be", mode)
	}
}
