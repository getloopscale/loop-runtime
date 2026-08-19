// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.1 of plan 2026-06-01-p1-security-hardening (sec-010 / CR-7 of
// MEET-2026-05-28-001) — per-tenant Fulcio audience for projected SA
// token. Tests lock the D4 dev-fallback: development → "sigstore" legacy
// shared; production/staging → "sigstore-<tenantID>" per-tenant binding;
// empty tenant in prod → sentinel that Fulcio rejects (fail-closed).

package argo

import (
	"testing"
)

// TestAudienceFor_ProductionPerTenant — staging + production return the
// per-tenant audience that Fulcio will validate against the tenant-bound
// allowlist (T1.2).
func TestAudienceFor_ProductionPerTenant(t *testing.T) {
	cases := []struct {
		env      string
		tenant   string
		expected string
	}{
		{"production", "tn-acme", "sigstore-tn-acme"},
		{"production", "tn-e2etest", "sigstore-tn-e2etest"},
		{"staging", "tn-corp", "sigstore-tn-corp"},
	}
	for _, c := range cases {
		got := audienceFor(c.tenant, c.env)
		if got != c.expected {
			t.Errorf("audienceFor(%q, %q): got %q want %q",
				c.tenant, c.env, got, c.expected)
		}
	}
}

// TestAudienceFor_DevelopmentLegacyFallback — development returns the
// legacy shared "sigstore" audience (D4 dev-fallback) so local cleanroom
// Fulcio (not yet upgraded for per-tenant binding) keeps working.
func TestAudienceFor_DevelopmentLegacyFallback(t *testing.T) {
	cases := []struct {
		env    string
		tenant string
	}{
		{"development", "tn-anything"},
		{"development", ""},
		{"", "tn-anything"},        // unknown env defaults to dev fallback
		{"unknown", "tn-anything"}, // ditto
	}
	for _, c := range cases {
		got := audienceFor(c.tenant, c.env)
		if got != "sigstore" {
			t.Errorf("audienceFor(%q, %q): got %q want %q (D4 dev fallback)",
				c.tenant, c.env, got, "sigstore")
		}
	}
}

// TestAudienceFor_ProdEmptyTenant_FailsClosed — empty tenantID in
// production/staging returns a sentinel that Fulcio will reject rather
// than silently falling back to legacy. Fail-closed posture: builds with
// missing tenant context fail at signing rather than producing a forgeable
// signature.
func TestAudienceFor_ProdEmptyTenant_FailsClosed(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		got := audienceFor("", env)
		if got != "sigstore-MISSING-TENANT" {
			t.Errorf("audienceFor(\"\", %q): got %q want sentinel sigstore-MISSING-TENANT",
				env, got)
		}
	}
}

// TestBuildVolumes_NonKeyless_NoProjectedToken — when CosignKeyless=false
// the projected SA token volume is NOT added (only registry creds + signing
// key). Regression guard against accidentally always-projecting.
func TestBuildVolumes_NonKeyless_NoProjectedToken(t *testing.T) {
	b := NewWorkflowBuilder(InfraParams{
		CosignKeyless: false,
		Env:           "development",
	})
	vols := b.buildVolumes("tn-acme")
	// 2 volumes expected (registry config + signing key); 0 projected.
	if len(vols) != 2 {
		t.Fatalf("non-keyless: expected 2 volumes (registry+key), got %d", len(vols))
	}
}

// TestBuildVolumes_Keyless_HasProjectedToken — when CosignKeyless=true
// the projected SA token volume IS appended (3 total).
func TestBuildVolumes_Keyless_HasProjectedToken(t *testing.T) {
	b := NewWorkflowBuilder(InfraParams{
		CosignKeyless: true,
		Env:           "production",
	})
	vols := b.buildVolumes("tn-acme")
	if len(vols) != 3 {
		t.Fatalf("keyless+prod: expected 3 volumes, got %d", len(vols))
	}
}
