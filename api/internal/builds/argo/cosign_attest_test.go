// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T2.1 + T2.3 of plan 2026-05-27-tenant-build-pipeline-gaps-closure-plan.md.
// Tests for cosignAttestTemplate + DAG wiring + idempotency assertion.

package argo

import (
	"strings"
	"testing"

	forge "github.com/getloopscale/argo-workflows-go"

	"github.com/getloopscale/loop-runtime/pkg/naming"
)

func TestCosignAttestTemplate_Renders(t *testing.T) {
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc123",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
		Keyless:       false,
	})

	if tmpl == nil {
		t.Fatal("expected non-nil template")
	}
	if tmpl.Name != "attest-image-api" {
		t.Errorf("name = %q, want attest-image-api", tmpl.Name)
	}
}

// v1.1 EC-NEW-6: attest step MUST have activeDeadlineSeconds bounded to
// Rekor append latency window. Original v1.0 had no timeout → build hang
// indefinite if Rekor degraded.
func TestCosignAttestTemplate_HasActiveDeadlineSeconds180(t *testing.T) {
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
	})

	if tmpl.ActiveDeadlineSeconds == nil {
		t.Fatal("activeDeadlineSeconds is nil — v1.1 EC-NEW-6 regression (Rekor latency unbounded)")
	}
	if *tmpl.ActiveDeadlineSeconds != 180 {
		t.Errorf("activeDeadlineSeconds = %d, want 180 (3min Rekor timeout)", *tmpl.ActiveDeadlineSeconds)
	}
}

// ADR D1: predicate type MUST be in-toto v1.0 CycloneDX standard URI.
// NOT custom LoopZero-specific type (compliance auditors recognize standards).
func TestCosignAttestTemplate_UsesCycloneDXPredicateType(t *testing.T) {
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
	})

	if !strings.Contains(tmpl.Source, cyclonedxPredicateType) {
		t.Errorf("script does not reference cyclonedxPredicateType %q", cyclonedxPredicateType)
	}
	if !strings.Contains(tmpl.Source, "--type") {
		t.Errorf("script missing --type flag for predicate type")
	}
}

func TestCosignAttestTemplate_ReadsImageDigestFromMetadata(t *testing.T) {
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "myapp",
		ImageTag:      "registry.test/org/myapp:v1",
		SBOMLocalPath: "/workspace/sbom-myapp.cdx.json",
	})

	if !strings.Contains(tmpl.Source, "metadata-myapp.json") {
		t.Error("script does not read digest from app-specific metadata file")
	}
	if !strings.Contains(tmpl.Source, "containerimage.digest") {
		t.Error("script does not extract containerimage.digest field")
	}
}

func TestCosignAttestTemplate_ReferencesSBOMFromSyftStep(t *testing.T) {
	sbomPath := "/workspace/sbom-myapp.cdx.json"
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "myapp",
		ImageTag:      "registry.test/org/myapp:v1",
		SBOMLocalPath: sbomPath,
	})

	if !strings.Contains(tmpl.Source, sbomPath) {
		t.Error("script does not reference SBOM path from syft step")
	}
	if !strings.Contains(tmpl.Source, "--predicate "+sbomPath) {
		t.Error("script does not pass SBOM as --predicate to cosign attest")
	}
}

// EC-1 (inline EC) — attestation requires SBOM. If syft step failed
// (skip-on-missing) and SBOM file absent, attest step must fail loud.
func TestCosignAttestTemplate_FailsLoudIfSBOMAbsent(t *testing.T) {
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
	})

	if !strings.Contains(tmpl.Source, "SBOM file not found") {
		t.Error("script does not fail-loud when SBOM file missing")
	}
}

// v1.1 EC-NEW-12 SHOULD TEST — cosign attest is idempotent for same
// digest per Sigstore docs. Same image rebuilt = same Rekor entry.
// This test documents the expectation; actual idempotency enforced by
// cosign + Rekor (we don't add extra logic).
func TestCosignAttestTemplate_IdempotentForSameDigest(t *testing.T) {
	tmpl1 := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
	})
	tmpl2 := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
	})

	// Same params → same script content (idempotent template generation).
	if tmpl1.Source != tmpl2.Source {
		t.Error("template generation is NOT idempotent for same params")
	}
	// Per Sigstore docs: cosign attest for same digest is no-op idempotent.
	// (Cannot test runtime here — documented expectation only.)
}

// ADR D1 + ec-1 inline: SLSA L3 requires linked attestation. Build MUST
// fail if attest fails (no continueOn at DAG level — workflow_builder enforces).
func TestCosignAttestTemplate_NoContinueOnFailure(t *testing.T) {
	// The template itself doesn't have continueOn — that's a DAG-level
	// task property. This test documents that the SCRIPT exits non-zero
	// when cosign attest returns non-zero, ensuring upstream DAG sees failure.
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
	})

	if !strings.Contains(tmpl.Source, "set -e") {
		t.Error("script must use `set -e` to propagate cosign exit code")
	}
	if !strings.Contains(tmpl.Source, "exit $ATTEST_EXIT") {
		t.Error("script must propagate cosign attest exit code (SLSA L3 chain requirement)")
	}
}

// Verify outputs include rekor-url for DB persistence (T2.2 migration).
func TestCosignAttestTemplate_ExportsRekorURL(t *testing.T) {
	tmpl := cosignAttestTemplate(CosignAttestParams{
		AppName:       "api",
		ImageTag:      "registry.test/org/api:abc",
		SBOMLocalPath: "/workspace/sbom-api.cdx.json",
	})

	hasRekorURL := false
	for _, out := range tmpl.Outputs {
		if out.Name == "rekor-url" {
			hasRekorURL = true
			break
		}
	}
	if !hasRekorURL {
		t.Error("template missing output parameter rekor-url (T2.2 DB persistence requirement)")
	}
}

// v1.1 EC-NEW-5: workflow_builder MUST insert attest task with deps on
// BOTH cosign-sign + sbom. Without sign dep, Rekor chain broken
// (attestation lands before sig → unlinked → SLSA L3 fail).
func TestWorkflowBuilder_AttestStepDependsOnSignAndSyft(t *testing.T) {
	// Arrange
	infra := testInfraParams()
	infra.CosignAttestEnabled = true
	infra.CosignKeyless = true
	infra.Env = "development"

	// Act
	wf, err := NewWorkflowBuilder(infra).Build(testSpec())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Assert
	var attest *forge.Task
	for _, tmpl := range wf.Templates {
		if dag, ok := tmpl.(*forge.DAG); ok && dag.Name == entrypointName {
			for _, task := range dag.Tasks {
				if task.Name == naming.AttestStepName("api") {
					attest = task
				}
			}
		}
	}
	if attest == nil {
		t.Fatal("no attest task in the build pipeline with CosignAttestEnabled")
	}
	deps := map[string]bool{}
	for _, d := range attest.Dependencies {
		deps[d] = true
	}
	for _, want := range []string{naming.SignStepName("api"), naming.SBOMStepName("api")} {
		if !deps[want] {
			t.Errorf("attest dependencies %v lack %q — the Rekor chain would break", attest.Dependencies, want)
		}
	}
}

// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1.
// CR-1 fix: gate `--insecure-skip-verify` flag behind explicit InsecureSkipSCT.

func TestCosignAttestScript_RendersInsecureFlag_WhenEnabled(t *testing.T) {
	c := cosignAttestTemplate(CosignAttestParams{
		AppName:         "api",
		ImageTag:        "registry.example.com/org/app:abc",
		SBOMLocalPath:   "/workspace/sbom-api.cdx.json",
		Keyless:         true,
		InsecureSkipSCT: true,
	})
	if !strings.Contains(c.Source, "--insecure-skip-verify") {
		t.Error("expected --insecure-skip-verify in rendered attest script when InsecureSkipSCT=true && Keyless=true")
	}
}

func TestCosignAttestScript_OmitsInsecureFlag_WhenDisabled(t *testing.T) {
	c := cosignAttestTemplate(CosignAttestParams{
		AppName:         "api",
		ImageTag:        "registry.example.com/org/app:abc",
		SBOMLocalPath:   "/workspace/sbom-api.cdx.json",
		Keyless:         true,
		InsecureSkipSCT: false,
	})
	if strings.Contains(c.Source, "--insecure-skip-verify") {
		t.Error("expected NO --insecure-skip-verify when InsecureSkipSCT=false (default)")
	}
}
