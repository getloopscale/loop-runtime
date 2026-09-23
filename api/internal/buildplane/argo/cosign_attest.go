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

	"github.com/getloopscale/loopzero/pkg/naming"
)

// CosignAttestParams configures the attestation step. AppName + ImageTag
// match the cosign-sign step naming convention; SBOMLocalPath references
// the syft step's output file (mounted via shared workspace).
type CosignAttestParams struct {
	AppName       string
	ImageTag      string
	SBOMLocalPath string // e.g., /workspace/sbom-{app}.cdx.json from syft step
	Keyless       bool   // mirror CosignKeyless InfraParams flag
	// InsecureRegistry mirror RegistryInsecure InfraParams flag — adds
	// --allow-http-registry --allow-insecure-registry flags for local
	// k3d HTTP-only registries. Discovered LIVE 2026-05-27 (build v5):
	// without these flags, cosign attest fails "http: server gave HTTP
	// response to HTTPS client" against k3d-loopzero-registry:5000.
	InsecureRegistry bool
	// FulcioURL + RekorURL used when Keyless=true. Empty → cosign defaults
	// (Sigstore public infra). Mirror CosignFulcioURL / CosignRekorURL InfraParams.
	FulcioURL string
	RekorURL  string
	// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1 — CR-1 fix.
	// InsecureSkipSCT renders `--insecure-skip-verify` flag in the cosign
	// attest command. ONLY relevant when Keyless=true. Default false.
	// EC-11 fix: flag omitted if Keyless=false regardless of this field.
	InsecureSkipSCT bool
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
	metadataFile := metadataFileFor(p.AppName)
	const outRekorURLPath = "/tmp/cosign-attest-rekor-url.txt"
	const oidcTokenPath = "/var/run/secrets/sigstore/token" //nolint:gosec // G101 false positive: filesystem PATH, not credential value

	// Build cosign attest command — keyed vs keyless variants per Keyless flag.
	// Keyless mode (M2 of plan 2026-05-27-M07-to-3-85): uses Fulcio short-lived
	// cert from projected SA OIDC token, NOT static --key. Discovered LIVE
	// 2026-05-27 attest pod FAIL — original ship sempre usava --key mesmo em keyless.
	// InsecureRegistry adds --allow-http-registry --allow-insecure-registry para k3d.
	insecureFlag := ""
	if p.InsecureRegistry {
		insecureFlag = " --allow-http-registry --allow-insecure-registry"
	}
	var attestSetup, attestCmd string
	if p.Keyless {
		fulcioURL := p.FulcioURL
		if fulcioURL == "" {
			fulcioURL = "https://fulcio.sigstore.dev"
		}
		rekorURL := p.RekorURL
		if rekorURL == "" {
			rekorURL = "https://rekor.sigstore.dev"
		}
		attestSetup = fmt.Sprintf(`# Keyless mode: read OIDC identity token for Fulcio.
if [ ! -r %s ]; then
  echo "ERROR: SA OIDC token not mounted at %s — keyless mode unavailable" >&2
  exit 1
fi
SIGSTORE_ID_TOKEN="$(cat %s)"
# 2026-05-28 Ralph iter 3: cosign v2.4.x flags directly (downgrade from v2.5+
# env-var-only to support simpler API + skip SCT for self-hosted CTLog).
FULCIO_URL="%s"
REKOR_URL="%s"
`, oidcTokenPath, oidcTokenPath, oidcTokenPath, fulcioURL, rekorURL)
		// T1.2 CR-1 fix: gate --insecure-skip-verify behind explicit InsecureSkipSCT.
		// EC-11 fix: only render when Keyless=true (this branch is keyless-only).
		skipSCTFlag := ""
		if p.InsecureSkipSCT {
			skipSCTFlag = "  --insecure-skip-verify \\\n"
		}
		attestCmd = fmt.Sprintf(`cosign attest --yes%s \
  --identity-token="$SIGSTORE_ID_TOKEN" \
  --fulcio-url="$FULCIO_URL" \
  --rekor-url="$REKOR_URL" \
%s  --predicate %s \
  --type %s \
  "%s@${DIGEST}" 2>&1`, insecureFlag, skipSCTFlag, p.SBOMLocalPath, cyclonedxPredicateType, p.ImageTag)
	} else {
		attestCmd = fmt.Sprintf(`cosign attest --yes%s \
  --key %s/cosign.key \
  --predicate %s \
  --type %s \
  "%s@${DIGEST}" 2>&1`, insecureFlag, cosignKeyMountPath, p.SBOMLocalPath, cyclonedxPredicateType, p.ImageTag)
	}

	script := fmt.Sprintf(`set -e
# v1.1 EC-NEW-5: this step MUST run AFTER cosign-sign-%s + sbom-%s (DAG dep).
# cosign attest associates attestation with EXISTING signature in Rekor.
# Running attest before sign produces unlinked Rekor entries → SLSA L3 FAILS.

# Extract digest from BuildKit metadata (same source as cosign-sign step).
DIGEST=$(%s)
if [ -z "$DIGEST" ]; then
  echo "ERROR: no digest found in metadata file" >&2
  exit 1
fi
echo "Attesting image: %s@${DIGEST}"
echo "SBOM source: %s"
echo "Predicate type: %s"
echo "Mode: keyless=%t"

# Verify SBOM file exists (syft step may have failed; without SBOM,
# attest is meaningless — fail loud).
if [ ! -f %s ]; then
  echo "ERROR: SBOM file not found at %s (syft step did not produce output)" >&2
  exit 1
fi

%s
# cosign attest --predicate creates in-toto attestation, signs it (key OR keyless),
# and uploads to Rekor. Rekor entry URL captured for DB persistence (T2.2).
# bash quirk fix: set -e + command substitution kills script on cosign fail
# BEFORE we can read $?. Disable -e around attest, capture exit explicitly.
set +e
ATTEST_OUTPUT=$(%s)
ATTEST_EXIT=$?
set -e
echo "$ATTEST_OUTPUT"

if [ $ATTEST_EXIT -ne 0 ]; then
  # v1.1 D1: SLSA L3 requires linked attestation. Build fails cleanly.
  # NOT continueOn — signed image without attestation is INSUFFICIENT.
  echo "ERROR: cosign attest failed (exit $ATTEST_EXIT) — build FAILS per SLSA L3 chain requirement" >&2
  exit $ATTEST_EXIT
fi

# Extract Rekor entry URL from cosign output. cosign v2.4.3 logs
# "tlog entry created with index: N" (no full URL). We reconstruct
# the canonical Rekor entry URL from the configured $REKOR_URL + index,
# falling back to scraping a literal URL if cosign ever emits one.
# Result is empty when no index/URL found (downstream gates on empty —
# NEVER emit "unknown" sentinel per M07 Ralph iter 5 enrichment rule).
REKOR_ENTRY_URL=$(echo "$ATTEST_OUTPUT" | sed -n 's/.*\(https*:\/\/rekor[^ ]*\).*/\1/p' | head -1)
if [ -z "$REKOR_ENTRY_URL" ]; then
  REKOR_INDEX=$(echo "$ATTEST_OUTPUT" | sed -n 's/.*tlog entry created with index:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)
  if [ -n "$REKOR_INDEX" ] && [ -n "${REKOR_URL:-}" ]; then
    REKOR_ENTRY_URL="${REKOR_URL%%/}/api/v1/log/entries?logIndex=${REKOR_INDEX}"
  fi
fi
printf '%%s' "${REKOR_ENTRY_URL}" > %s

echo "Attestation complete; Rekor entry: ${REKOR_ENTRY_URL:-<none>}"
`, p.AppName, p.AppName, digestFromMetadata(metadataFile), p.ImageTag, p.SBOMLocalPath,
		cyclonedxPredicateType, p.Keyless, p.SBOMLocalPath, p.SBOMLocalPath,
		attestSetup, attestCmd, outRekorURLPath)

	return &forge.Script{
		Name:    naming.AttestStepName(p.AppName),
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
		VolumeMounts: attestVolumeMounts(p.Keyless),
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

// attestVolumeMounts returns the volume mounts for the cosign-attest step.
// When keyless=true, mounts the workflow-level sigstore-token projected
// volume at /var/run/secrets/sigstore so cosign can read SIGSTORE_ID_TOKEN.
// The sigstore-token volume is injected pod-level by workflow_builder.buildVolumes()
// when InfraParams.CosignKeyless=true.
func attestVolumeMounts(keyless bool) []forge.VolumeBuilder {
	mounts := []forge.VolumeBuilder{
		// Shared /workspace PVC with syft step (SBOM file source via PVC).
		&forge.PVCVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
		&forge.SecretVolume{BaseVolume: forge.BaseVolume{
			Name: secretRegistryConfig, MountPath: buildkitRegistryMount, ReadOnly: true,
		}, SecretName: secretRegistryConfig},
		&forge.SecretVolume{BaseVolume: forge.BaseVolume{
			Name: secretCosignKey, MountPath: cosignKeyMountPath, ReadOnly: true,
		}, SecretName: secretCosignKey},
	}
	if keyless {
		// Reference the pod-level projected SA token volume injected by
		// workflow_builder.buildVolumes() when CosignKeyless=true.
		// VolumeMount only — the actual volume definition is at workflow level.
		mounts = append(mounts, &forge.SecretVolume{BaseVolume: forge.BaseVolume{
			Name: sigstoreTokenVolumeName, MountPath: sigstoreTokenMountPath, ReadOnly: true,
		}, SecretName: sigstoreTokenVolumeName}) // SecretName ignored — name refers to projected vol
	}
	return mounts
}
