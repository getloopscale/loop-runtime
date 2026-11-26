// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T1.1 of plan 2026-06-01-p1-security-hardening (sec-010 / CR-7 of
// MEET-2026-05-28-001) — per-tenant Fulcio audience for projected SA
// token. Tests lock the D4 dev-fallback: development → "sigstore" legacy
// shared; production/staging → "sigstore-<tenantID>" per-tenant binding;
// empty tenant in prod, or any environment not named, is an error.

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
		got, err := audienceFor(c.tenant, c.env)
		if err != nil || got != c.expected {
			t.Errorf("audienceFor(%q, %q): got %q want %q",
				c.tenant, c.env, got, c.expected)
		}
	}
}

// TestAudienceFor_DevelopmentLegacyFallback — development returns the
// legacy shared "sigstore" audience (D4 dev-fallback) so local cleanroom
// Fulcio (not yet upgraded for per-tenant binding) keeps working.
func TestAudienceFor_DevelopmentLegacyFallback(t *testing.T) {
	for _, tenant := range []string{"tn-anything", ""} {
		got, err := audienceFor(tenant, "development")
		if err != nil || got != "sigstore" {
			t.Errorf("audienceFor(%q, development) = (%q, %v), want (sigstore, nil)", tenant, got, err)
		}
	}
}

// TestAudienceFor_UnknownEnvironmentIsAnError — only an explicitly
// development environment may share the legacy audience. A typo or a new
// environment name used to fall through to "sigstore" and silently drop the
// per-tenant signing isolation.
func TestAudienceFor_UnknownEnvironmentIsAnError(t *testing.T) {
	for _, env := range []string{"", "unknown", "prod", "Production"} {
		if got, err := audienceFor("tn-anything", env); err == nil {
			t.Errorf("audienceFor(tn-anything, %q) = %q, want an error", env, got)
		}
	}
}

// TestAudienceFor_ProdEmptyTenant_FailsClosed — an empty tenantID in
// production/staging is an error, not a sentinel audience to be rejected
// later by Fulcio.
func TestAudienceFor_ProdEmptyTenant_FailsClosed(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		if got, err := audienceFor("", env); err == nil {
			t.Errorf("audienceFor(\"\", %q) = %q, want an error", env, got)
		}
	}
}

// TestBuild_KeylessWithUnknownEnvironmentFails — the error reaches the caller
// instead of a workflow signed under the shared audience.
func TestBuild_KeylessWithUnknownEnvironmentFails(t *testing.T) {
	infra := testInfraParams()
	infra.CosignKeyless = true
	infra.Env = "prodution"
	if _, err := NewWorkflowBuilder(infra).Build(testSpec()); err == nil {
		t.Fatal("Build succeeded with a keyless signing audience it could not resolve")
	}
}

// TestBuild_InvalidRegistryURLIsAnErrorNotAPanic — the registry URL is
// configuration; a bad value must fail the build request, not crash the API
// goroutine that builds the workflow.
func TestBuild_InvalidRegistryURLIsAnErrorNotAPanic(t *testing.T) {
	infra := testInfraParams()
	infra.RegistryURL = "registry\"\n[evil]"
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Build panicked: %v", r)
		}
	}()
	if _, err := NewWorkflowBuilder(infra).Build(testSpec()); err == nil {
		t.Fatal("Build accepted a registry URL that breaks buildkitd.toml")
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
	vols, err := b.buildVolumes("tn-acme")
	if err != nil {
		t.Fatal(err)
	}
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
	vols, err := b.buildVolumes("tn-acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 3 {
		t.Fatalf("keyless+prod: expected 3 volumes, got %d", len(vols))
	}
}
