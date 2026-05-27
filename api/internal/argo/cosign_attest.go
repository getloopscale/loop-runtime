// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure-plan.md.
// Per ADR D1: `cosign attest --predicate <sbom>` creates SLSA L3 linked
// attestation between signed image + CycloneDX SBOM. Industry standard
// in-toto v1.0 + Sigstore Public Good Bundle Spec.
//
// v1.1 EC-NEW-6: activeDeadlineSeconds: 180 to bound Rekor append latency
//   (unbounded cross-witness + transparency log throughput → would hang
//    Workflow indefinitely without timeout).
//
// v1.1 EC-NEW-5: DAG dependency MUST be on BOTH cosign-sign AND syft
//   tasks (wired in workflow_builder.go). cosign attest semantically
//   associates with EXISTING signature in Rekor; running attest before
//   sign produces unlinked Rekor entries → SLSA L3 verification FAILS.

package argo

import (
	"fmt"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"
)

// CosignAttestParams configures the attestation step. AppName + ImageTag
// match the cosign-sign step naming convention; SBOMLocalPath references
// the syft step's output file (mounted via shared workspace).
type CosignAttestParams struct {
	AppName       string
	ImageTag      string
	SBOMLocalPath string // e.g., /workspace/sbom-{app}.cdx.json from syft step
	Keyless       bool   // mirror CosignKeyless InfraParams flag
	// CyclonedxPredicateType is the in-toto predicate type URI.
	// Per ADR D1: https://cyclonedx.org/bom (in-toto + SLSA standard).
	// Hardcoded constant to prevent operator drift.
}

// cyclonedxPredicateType is the in-toto v1.0 predicate type for CycloneDX
// SBOM attestation per https://github.com/in-toto/attestation/blob/main/spec/predicates/cyclonedx.md
const cyclonedxPredicateType = "https://cyclonedx.org/bom"

// attestStepActiveDeadlineSeconds is the timeout for the cosign attest
// step. v1.1 EC-NEW-6: Rekor append latency unbounded; 3min matches
// existing cosign-sign timeout pattern.
const attestStepActiveDeadlineSeconds = 180

// cosignAttestTemplate produces an Argo Workflow step that runs
// `cosign attest --predicate <sbom> --type cyclonedx ... <image>@<digest>`.
// The resulting attestation is logged to Rekor (linked with the image's
// existing signature) — SLSA L3 chain complete.
//
// Sourcing semantics:
//   - Image digest: read from BuildKit metadata file (same as cosignSign)
//   - SBOM bytes: read from sbomLocalPath (syft step output)
//   - Signing key: same as cosign-sign step (kms-key OR keyless)
//   - Output: Rekor entry URL written to /tmp/cosign-attest-rekor-url.txt,
//     captured as Argo workflow output param for downstream persistence
//     in releases.image_attestation_rekor_url DB column (T2.2 migration).
func cosignAttestTemplate(p CosignAttestParams) *forge.Script {
	metadataFile := fmt.Sprintf("%s/metadata-%s.json", workspaceMountPath, p.AppName)
	const outRekorURLPath = "/tmp/cosign-attest-rekor-url.txt"

	insecureFlag := ""
	if p.Keyless {
		// Keyless mode: no insecure flag needed (Fulcio is HTTPS).
	}

	script := fmt.Sprintf(`set -e
# v1.1 EC-NEW-5: this step MUST run AFTER cosign-sign-%s + sbom-%s (DAG dep).
# cosign attest associates attestation with EXISTING signature in Rekor.
# Running attest before sign produces unlinked Rekor entries → SLSA L3 FAILS.

# Extract digest from BuildKit metadata (same source as cosign-sign step).
DIGEST=$(sed -n 's/.*"containerimage.digest":[[:space:]]*"\([^"]*\)".*/\1/p' %s)
if [ -z "$DIGEST" ]; then
  echo "ERROR: no digest found in metadata file" >&2
  exit 1
fi
echo "Attesting image: %s@${DIGEST}"
echo "SBOM source: %s"
echo "Predicate type: %s"

# Verify SBOM file exists (syft step may have failed; without SBOM,
# attest is meaningless — fail loud).
if [ ! -f %s ]; then
  echo "ERROR: SBOM file not found at %s (syft step did not produce output)" >&2
  exit 1
fi

# cosign attest --predicate creates in-toto attestation, signs it with
# the same key as cosign-sign (key OR keyless), and uploads to Rekor.
# The Rekor entry URL is captured for DB persistence (T2.2).
ATTEST_OUTPUT=$(cosign attest --yes%s \
  --key %s/cosign.key \
  --predicate %s \
  --type %s \
  "%s@${DIGEST}" 2>&1)

ATTEST_EXIT=$?
echo "$ATTEST_OUTPUT"

if [ $ATTEST_EXIT -ne 0 ]; then
  # v1.1 D1: SLSA L3 requires linked attestation. Build fails cleanly.
  # NOT continueOn — signed image without attestation is INSUFFICIENT.
  echo "ERROR: cosign attest failed (exit $ATTEST_EXIT) — build FAILS per SLSA L3 chain requirement" >&2
  exit $ATTEST_EXIT
fi

# Extract Rekor entry URL from cosign output. Format varies by cosign
# version; v2.2+ logs "tlog entry created with index: N" + provides URL.
REKOR_URL=$(echo "$ATTEST_OUTPUT" | sed -n 's/.*\(https:\/\/rekor[^ ]*\).*/\1/p' | head -1)
if [ -z "$REKOR_URL" ]; then
  # Fallback: best-effort — empty URL acceptable if cosign didn't log it.
  REKOR_URL="unknown"
fi
printf '%%s' "${REKOR_URL}" > %s

echo "Attestation complete; Rekor entry: ${REKOR_URL}"
`, p.AppName, p.AppName, metadataFile, p.ImageTag, p.SBOMLocalPath,
		cyclonedxPredicateType, p.SBOMLocalPath, p.SBOMLocalPath,
		insecureFlag, cosignKeyMountPath, p.SBOMLocalPath, cyclonedxPredicateType,
		p.ImageTag, outRekorURLPath)

	return &forge.Script{
		Name:    fmt.Sprintf("attest-image-%s", p.AppName),
		Image:   loopzeroCosignImage(),
		Command: []string{"sh"},
		Source:  script,
		// v1.1 EC-NEW-6: activeDeadlineSeconds bounds Rekor latency.
		ActiveDeadlineSeconds: forge.Ptr(attestStepActiveDeadlineSeconds),
		Outputs: []forge.Parameter{
			{
				Name:      "rekor-url",
				ValueFrom: &model.ValueFrom{Path: outRekorURLPath},
			},
		},
		Env: []forge.EnvBuilder{
			// HOME + TUF_ROOT writable (same pattern as cosign-sign).
			forge.Env{Name: "HOME", Value: "/tmp"},
			forge.Env{Name: "TUF_ROOT", Value: "/tmp/.sigstore/root"},
			// COSIGN_PASSWORD from secret (shared with cosign-sign).
			forge.SecretEnv{Name: "COSIGN_PASSWORD", SecretName: secretCosignKey, SecretKey: "cosign.password"},
			// Docker config for Rekor push auth.
			forge.Env{Name: "DOCKER_CONFIG", Value: buildkitRegistryMount},
		},
		VolumeMounts: []forge.VolumeBuilder{
			// Shared /workspace with syft step (SBOM file source).
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretRegistryConfig, MountPath: buildkitRegistryMount, ReadOnly: true,
			}, SecretName: secretRegistryConfig},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretCosignKey, MountPath: cosignKeyMountPath, ReadOnly: true,
			}, SecretName: secretCosignKey},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "100m", Memory: "128Mi"},
			Limits:   forge.ResourceList{CPU: "500m", Memory: "256Mi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsUser:    forge.Ptr(rootlessUID),
			RunAsGroup:   forge.Ptr(rootlessUID),
			RunAsNonRoot: forge.Ptr(true),
		},
	}
}
