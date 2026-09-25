// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// CHG-2026-04-25-ATTESTATION-WIRE Phase 4 (AC-4.1):
// when the workflow surfaces signature + digest outputs for an app,
// the corresponding AppRelease entry must be enriched with those
// values plus a server-side SignedAt timestamp.
func TestEnrichAppReleasesWithAttestation_PopulatesSignatureFields(t *testing.T) {
	apps := []deploycontracts.AppRelease{
		{Name: "api", URL: "https://api.example.com"},
		{Name: "web", URL: "https://web.example.com"},
	}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api": {
				"signature": "registry.example.com/org/api:abc@sha256:deadbeef",
				"digest":    "sha256:deadbeef",
			},
			"sign-image-web": {
				"signature": "registry.example.com/org/web:abc@sha256:cafebabe",
				"digest":    "sha256:cafebabe",
			},
		},
	}

	before := time.Now().UTC()
	enrichAppReleasesWithAttestation(apps, status)
	after := time.Now().UTC().Add(time.Second)

	if apps[0].ImageSignature != "registry.example.com/org/api:abc@sha256:deadbeef" {
		t.Errorf("api signature: got %q", apps[0].ImageSignature)
	}
	if apps[0].ImageDigestSHA256 != "sha256:deadbeef" {
		t.Errorf("api digest: got %q", apps[0].ImageDigestSHA256)
	}
	if apps[0].ImageSignedAt == nil {
		t.Fatal("api ImageSignedAt is nil")
	}
	if apps[0].ImageSignedAt.Before(before) || apps[0].ImageSignedAt.After(after) {
		t.Errorf("api ImageSignedAt %v outside expected window [%v, %v]", apps[0].ImageSignedAt, before, after)
	}

	if apps[1].ImageSignature != "registry.example.com/org/web:abc@sha256:cafebabe" {
		t.Errorf("web signature: got %q", apps[1].ImageSignature)
	}
}

// AC-4.2: workflows without outputs (legacy / pre-CHG) must still
// produce valid AppReleases with empty signature fields. The deploy
// pipeline must NOT break for these.
func TestEnrichAppReleasesWithAttestation_NoOutputs_LeavesAppsUntouched(t *testing.T) {
	apps := []deploycontracts.AppRelease{
		{Name: "api", URL: "https://api.example.com", ImageDigest: "registry/api:abc"},
	}
	cases := []*deploycontracts.WorkflowStatus{
		nil,
		{Phase: "Succeeded"},               // no outputs map
		{Phase: "Succeeded", Outputs: nil}, // explicit nil
		{Phase: "Succeeded", Outputs: map[string]map[string]string{}}, // empty
		{
			Phase: "Succeeded",
			Outputs: map[string]map[string]string{
				"clone-repo": {"some": "value"}, // outputs but not for our apps
			},
		},
	}
	for i, status := range cases {
		// Reset
		apps[0].ImageSignature = ""
		apps[0].ImageDigestSHA256 = ""
		apps[0].ImageSignedAt = nil

		enrichAppReleasesWithAttestation(apps, status)

		if apps[0].ImageSignature != "" {
			t.Errorf("case %d: signature should be empty, got %q", i, apps[0].ImageSignature)
		}
		if apps[0].ImageDigestSHA256 != "" {
			t.Errorf("case %d: digest should be empty, got %q", i, apps[0].ImageDigestSHA256)
		}
		if apps[0].ImageSignedAt != nil {
			t.Errorf("case %d: SignedAt should be nil, got %v", i, apps[0].ImageSignedAt)
		}
		// Existing fields preserved
		if apps[0].ImageDigest != "registry/api:abc" {
			t.Errorf("case %d: legacy ImageDigest field corrupted: %q", i, apps[0].ImageDigest)
		}
	}
}

// AC-4.3: SignedAt is set only when Signature is non-empty.
// A digest-without-signature is meaningless from a trust standpoint
// and SignedAt must NOT lie about it.
func TestEnrichAppReleasesWithAttestation_DigestWithoutSignature_NoSignedAt(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"build-image-api": {"digest": "sha256:abc"}, // no signature
		},
	}

	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].ImageDigestSHA256 != "sha256:abc" {
		t.Errorf("digest should be populated from build-image fallback: got %q", apps[0].ImageDigestSHA256)
	}
	if apps[0].ImageSignature != "" {
		t.Errorf("signature should be empty, got %q", apps[0].ImageSignature)
	}
	if apps[0].ImageSignedAt != nil {
		t.Errorf("SignedAt must be nil when no signature was captured: got %v", apps[0].ImageSignedAt)
	}
}

// AC-4.3 cont: the cosign step's digest takes precedence over the
// build-image step's digest, because the cosign step signed the bytes
// that the cosign-step digest refers to.
func TestEnrichAppReleasesWithAttestation_CosignDigestPreferredOverBuild(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api": {
				"signature": "registry/api:abc@sha256:cosign",
				"digest":    "sha256:cosign",
			},
			"build-image-api": {"digest": "sha256:buildkit"},
		},
	}

	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].ImageDigestSHA256 != "sha256:cosign" {
		t.Errorf("expected cosign digest to win, got %q", apps[0].ImageDigestSHA256)
	}
}

// AC-4.3 cont: when only build-image has a digest (no cosign step ran,
// or signing was skipped), the build-image digest is used as fallback.
// Useful for workflows where signing is opt-out.
func TestEnrichAppReleasesWithAttestation_FallsBackToBuildkitDigest(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"build-image-api": {"digest": "sha256:onlybuild"},
		},
	}

	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].ImageDigestSHA256 != "sha256:onlybuild" {
		t.Errorf("expected fallback to build digest, got %q", apps[0].ImageDigestSHA256)
	}
	if apps[0].ImageSignedAt != nil {
		t.Error("SignedAt must be nil when no cosign signature was captured")
	}
}

// =============================================================================
// Sprint 6 / T3.1 — SBOM extraction
// =============================================================================

func TestEnrichAppReleases_WithSBOM(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api": {
				"signature": "registry/api:abc@sha256:dead",
				"digest":    "sha256:dead",
			},
			"sbom-api": {
				"sbom_s3_key":          "sbom/tn-1/proj/abc/api.cdx.json",
				"sbom_format":          "cyclonedx-json",
				"sbom_component_count": "142",
			},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].SBOMS3Key != "sbom/tn-1/proj/abc/api.cdx.json" {
		t.Errorf("SBOMS3Key = %q", apps[0].SBOMS3Key)
	}
	if apps[0].SBOMFormat != "cyclonedx-json" {
		t.Errorf("SBOMFormat = %q", apps[0].SBOMFormat)
	}
	if apps[0].SBOMComponentCount != 142 {
		t.Errorf("SBOMComponentCount = %d, want 142", apps[0].SBOMComponentCount)
	}
	// Signature still works alongside SBOM.
	if apps[0].ImageSignature == "" {
		t.Error("signature must still be populated when SBOM is also present")
	}
}

// SBOM step is best-effort. When absent (continueOn.failed at the DAG
// level + scan failure), the apps array must still be valid and signatures
// preserved. No error, no panic.
func TestEnrichAppReleases_SBOMMissing_GracefulDegradation(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api": {
				"signature": "registry/api:abc@sha256:dead",
				"digest":    "sha256:dead",
			},
			// no sbom-api key at all
		},
	}
	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].SBOMS3Key != "" {
		t.Errorf("SBOMS3Key should be empty, got %q", apps[0].SBOMS3Key)
	}
	if apps[0].SBOMComponentCount != 0 {
		t.Errorf("SBOMComponentCount should be 0, got %d", apps[0].SBOMComponentCount)
	}
	if apps[0].ImageSignature == "" {
		t.Error("signature must remain populated even without SBOM")
	}
}

// Bad component count (non-integer) must not corrupt the AppRelease.
func TestEnrichAppReleases_SBOMInvalidCount_SafeDefault(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sbom-api": {
				"sbom_s3_key":          "sbom/key",
				"sbom_format":          "cyclonedx-json",
				"sbom_component_count": "garbage",
			},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].SBOMComponentCount != 0 {
		t.Errorf("invalid count must default to 0, got %d", apps[0].SBOMComponentCount)
	}
	if apps[0].SBOMS3Key != "sbom/key" {
		t.Errorf("S3 key still extracted: %q", apps[0].SBOMS3Key)
	}
}

// SBOM enriches multi-app builds independently per app.
func TestEnrichAppReleases_BothSignatureAndSBOM_MultiApp(t *testing.T) {
	apps := []deploycontracts.AppRelease{
		{Name: "api"},
		{Name: "worker"},
	}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api":    {"signature": "registry/api:abc@sha256:a", "digest": "sha256:a"},
			"sign-image-worker": {"signature": "registry/worker:abc@sha256:w", "digest": "sha256:w"},
			"sbom-api":          {"sbom_s3_key": "k-api", "sbom_format": "cyclonedx-json", "sbom_component_count": "10"},
			// worker SBOM is intentionally missing — should not affect api
		},
	}
	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].SBOMS3Key != "k-api" {
		t.Errorf("api SBOMS3Key = %q", apps[0].SBOMS3Key)
	}
	if apps[1].SBOMS3Key != "" {
		t.Errorf("worker SBOMS3Key should be empty, got %q", apps[1].SBOMS3Key)
	}
	if apps[1].ImageSignature == "" {
		t.Error("worker signature must still be populated")
	}
}

// M07 Ralph iter 5: attest-image step writes "rekor-url" output parameter.
// enrichAppReleasesWithAttestation MUST populate
// BuildAppOutputs.ImageAttestationRekorURL so container_delivery.go can
// persist it into releases.image_attestation_rekor_url (migration 000019).
//
// Plan 2026-05-28-M07-fulcio-cacert-keyless-LIVE v1.6 DoD item 9:
// "Runtime-metric proof — releases.image_attestation_rekor_url populated
// LIVE in Phase 4 = runtime evidence".
func TestEnrichAppReleasesWithAttestation_PopulatesRekorURL(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api", URL: "https://api.example.com"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api": {
				"signature": "registry/api:abc@sha256:dead",
				"digest":    "sha256:dead",
			},
			"attest-image-api": {
				"rekor-url": "https://rekor.sigstore.dev/api/v1/log/entries?logIndex=12345",
			},
		},
	}

	enrichAppReleasesWithAttestation(apps, status)

	want := "https://rekor.sigstore.dev/api/v1/log/entries?logIndex=12345"
	if apps[0].ImageAttestationRekorURL != want {
		t.Errorf("ImageAttestationRekorURL: got %q, want %q",
			apps[0].ImageAttestationRekorURL, want)
	}
}

// "unknown" is the cosign_attest.go fallback when sed pattern doesn't match.
// Persisting "unknown" pollutes the DB — must stay empty.
func TestEnrichAppReleasesWithAttestation_RekorURLUnknown_LeavesEmpty(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": "unknown"},
		},
	}

	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].ImageAttestationRekorURL != "" {
		t.Errorf("ImageAttestationRekorURL: got %q, expected empty when 'unknown'",
			apps[0].ImageAttestationRekorURL)
	}
}

// Missing attest step output (e.g., keyed mode without attest, or attest
// step failed and never wrote the param) MUST leave the field empty —
// downstream container_delivery.go gates on empty before persisting.
func TestEnrichAppReleasesWithAttestation_AttestStepMissing_NoOp(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api", ImageAttestationRekorURL: ""}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api": {"signature": "registry/api@sha256:x", "digest": "sha256:x"},
		},
	}

	enrichAppReleasesWithAttestation(apps, status)

	if apps[0].ImageAttestationRekorURL != "" {
		t.Errorf("ImageAttestationRekorURL: got %q, expected empty when attest step absent",
			apps[0].ImageAttestationRekorURL)
	}
}
