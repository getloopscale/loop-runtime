// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"
	"os"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"
)

// loopzeroCosignImage returns the loopzero-cosign-signer image, allowing override via env var.
// Local dev sets LOOPZERO_COSIGN_IMAGE to use the k3d registry image.
// Mirrors the loopzeroPacks() pattern in languages.go (NEW-11 fix 2026-04-30:
// dogfood E2E unblock surfaced that DOCR images were hardcoded for cosign +
// syft, with the override pattern only applied to packs-runner. Without
// this, local builds always 401 against DOCR for the sign-image step).
func loopzeroCosignImage() string {
	if v := os.Getenv("LOOPZERO_COSIGN_IMAGE"); v != "" {
		return v
	}
	return imageCosignSigner
}

// loopzeroSyftImage returns the loopzero-syft-scanner image, allowing override via env var.
// Local dev sets LOOPZERO_SYFT_IMAGE to use the k3d registry image.
func loopzeroSyftImage() string {
	if v := os.Getenv("LOOPZERO_SYFT_IMAGE"); v != "" {
		return v
	}
	return imageSyftScanner
}

// CosignSignParams configures the cosign signing template behavior.
// Plan T4.1 + MEET-2026-05-03-063 (MEET-B): CODE supports both signing modes;
// runtime activation of keyless is deferred to Sprint 9+ until cluster OIDC
// infrastructure is provisioned.
type CosignSignParams struct {
	AppName           string
	ImageTag          string
	InsecureRegistry  bool
	Keyless           bool   // true = OIDC + Fulcio + Rekor; false = key-based
	FulcioURL         string // optional override; empty = sigstore.dev public Fulcio
	RekorURL          string // optional override; empty = sigstore.dev public Rekor
}

// cosignSignTemplate generates an inline container template that signs a
// container image with cosign after BuildKit pushes it.
//
// Sprint 6: Supply chain security. Every user image is signed at build time.
// Sprint 9+: keyless mode (Plan T4.1 Track-2) — OIDC token mounted via projected
// service account, Fulcio issues short-lived cert, Rekor logs entry.
//
// Mode selection (`params.Keyless`):
//   - false (default): key-based — `cosign-signing-key` Secret mounts cosign.key + cosign.password.
//   - true:            keyless — projected SA token mounted at /var/run/secrets/sigstore;
//                      cosign reads it via SIGSTORE_ID_TOKEN env var; Fulcio issues cert;
//                      Rekor records entry. No long-lived key material in the cluster.
//
// The signing step reads the image digest from the BuildKit metadata file
// (written to /workspace/metadata-{appName}.json) and signs by digest,
// ensuring the signature is pinned to the exact image content.
//
// Image choice: Uses pre-built loopzero-cosign-signer image with cosign baked in.
// SEC-006: No runtime binary downloads — cosign is checksum-verified at image build time.
//
// B7 (CHG-2026-04-22-001): insecureRegistry mirrors the same toggle used by BuildKit
// (InfraParams.RegistryInsecure). When true, cosign receives --allow-insecure-registry
// so it speaks HTTP (local k3d dev). Config.Validate() enforces false in production.
func cosignSignTemplate(appName, imageTag string, insecureRegistry bool) *forge.Container {
	return cosignSignTemplateWithParams(CosignSignParams{
		AppName:          appName,
		ImageTag:         imageTag,
		InsecureRegistry: insecureRegistry,
		Keyless:          false, // default: backward-compatible key-based signing
	})
}

// cosignSignTemplateWithParams is the new explicit-params variant supporting
// both key-based and keyless modes. Plan T4.1 entry point for callers that
// want to opt into keyless. The legacy 3-arg cosignSignTemplate is kept as a
// thin wrapper to avoid touching ~10 existing call sites unnecessarily.
func cosignSignTemplateWithParams(p CosignSignParams) *forge.Container {
	if p.Keyless {
		return cosignSignTemplateKeyless(p)
	}
	return cosignSignTemplateKeyBased(p.AppName, p.ImageTag, p.InsecureRegistry)
}

// cosignSignTemplateKeyBased is the original key-based signing template.
// Renamed-extracted from `cosignSignTemplate` to make the mode-split explicit.
func cosignSignTemplateKeyBased(appName, imageTag string, insecureRegistry bool) *forge.Container {
	metadataFile := fmt.Sprintf("%s/metadata-%s.json", workspaceMountPath, appName)

	// cosign v3 distinguishes: --allow-insecure-registry (self-signed TLS) from
	// --allow-http-registry (plain HTTP). Local k3d registry serves plain HTTP.
	insecureFlag := ""
	if insecureRegistry {
		insecureFlag = " --allow-http-registry --allow-insecure-registry"
	}

	// CHG-2026-04-25-ATTESTATION-WIRE: the script MUST write the
	// signature reference and the cryptographic digest to two
	// container-local files. Argo captures them as workflow output
	// parameters via the Outputs declaration below, so the API can
	// persist them on the release row even after this pod terminates.
	//
	// Trust model: both paths live in the cosign container's OWN /tmp
	// filesystem. They are NOT on the shared /workspace volume — a
	// non-cosign container in the same pod (e.g., BuildKit) cannot
	// forge a signature by writing to these paths.
	const (
		outSignaturePath = "/tmp/cosign-sig.txt"
		outDigestPath    = "/tmp/cosign-digest.txt"
	)

	// Shell script that:
	// 1. Extracts the image digest from BuildKit metadata using sed
	// 2. Signs the image by digest using the pre-installed cosign binary
	// 3. Writes the pinned reference + digest to the workflow output paths
	// The COSIGN_PASSWORD env var is injected from the secret.
	script := fmt.Sprintf(`set -e
# Extract digest from BuildKit metadata using sed.
# BuildKit writes: {"containerimage.digest":"sha256:abc123..."}
DIGEST=$(sed -n 's/.*"containerimage.digest":[[:space:]]*"\([^"]*\)".*/\1/p' %s)
if [ -z "$DIGEST" ]; then
  echo "ERROR: no digest found in metadata file" >&2
  exit 1
fi
echo "Signing image: %s@${DIGEST}"

# Sign with cosign (key-based — cluster does not have OIDC provider for keyless)
# LOCAL ONLY (LP-003): k3d service account tokens não aceitos por public Fulcio.
# Production GHA path (helm-charts.yml) uses keyless. Track-2 (in-cluster Argo) deferred to Sprint 9+ per MEET-2026-05-03-063.
cosign sign --yes%s --key %s/cosign.key \
  -a "build-system=loopzero" \
  -a "app=%s" \
  "%s@${DIGEST}"

# Persist the signature reference and digest as Argo workflow outputs.
# These files are read AFTER cosign returns 0; if cosign fails, the
# script exits earlier and the outputs stay unset (Argo reports the
# pod failed and downstream sees no signature).
printf '%%s' "%s@${DIGEST}" > %s
printf '%%s' "${DIGEST}"   > %s

echo "Image signed successfully"
`, metadataFile, imageTag, insecureFlag, cosignKeyMountPath, appName, imageTag,
		imageTag, outSignaturePath, outDigestPath)

	return &forge.Container{
		Name:    fmt.Sprintf("sign-image-%s", appName),
		Image:   loopzeroCosignImage(),
		Command: []string{"sh", "-c", script},
		Outputs: []forge.Parameter{
			{
				Name:      "signature",
				ValueFrom: &model.ValueFrom{Path: outSignaturePath},
			},
			{
				Name:      "digest",
				ValueFrom: &model.ValueFrom{Path: outDigestPath},
			},
		},
		Env: []forge.EnvBuilder{
			// Bug #39: cosign needs HOME writable for TUF cache (/.sigstore).
			// /home/user is non-writable in the signer image under PSA restricted.
			// /tmp is always writable. TUF_ROOT also honored by cosign.
			forge.Env{Name: "HOME", Value: "/tmp"},
			forge.Env{Name: "TUF_ROOT", Value: "/tmp/.sigstore/root"},
			// COSIGN_PASSWORD is read from the secret mounted as env.
			forge.SecretEnv{Name: "COSIGN_PASSWORD", SecretName: secretCosignKey, SecretKey: "cosign.password"},
			// Docker config for registry auth — cosign needs this to push signatures.
			forge.Env{Name: "DOCKER_CONFIG", Value: buildkitRegistryMount},
		},
		VolumeMounts: []forge.VolumeBuilder{
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
			RunAsUser:    ptr(rootlessUID),
			RunAsGroup:   ptr(rootlessUID),
			RunAsNonRoot: ptr(true),
		},
	}
}

// cosignSignTemplateKeyless renders the keyless variant of the signing step.
// Plan T4.1 Track-2 + INF-G16 + MEET-2026-05-03-063.
//
// Trust model: the workflow pod has a projected service account token mounted
// at /var/run/secrets/sigstore. Cosign reads it via SIGSTORE_ID_TOKEN and presents
// it to Fulcio (default: https://fulcio.sigstore.dev) which issues a short-lived
// X.509 certificate bound to the SA identity. The signature + cert are recorded
// in Rekor (default: https://rekor.sigstore.dev) for transparency.
//
// Runtime requirements (Sprint 9+ deferred per MEET-B):
//   1. Build cluster service account configured as OIDC issuer accepted by
//      Fulcio. EITHER public Fulcio with custom issuer trust list (currently
//      not supported by sigstore.dev public Fulcio) OR private Fulcio
//      deployment trusting the cluster SA OIDC discovery doc OR Spire-issued
//      SVID providing SPIFFE identity bridged to Fulcio.
//   2. Cluster has egress to Fulcio + Rekor URLs.
//   3. NetworkPolicy allows egress from build namespace to Fulcio/Rekor.
//
// Until those are met, callers should leave InfraParams.CosignKeyless=false
// (the default), which routes through cosignSignTemplateKeyBased above.
//
// The CODE is delivered (Plan P4.1 closure); runtime activation tracks
// Sprint 9 readiness review.
func cosignSignTemplateKeyless(p CosignSignParams) *forge.Container {
	metadataFile := fmt.Sprintf("%s/metadata-%s.json", workspaceMountPath, p.AppName)

	insecureFlag := ""
	if p.InsecureRegistry {
		insecureFlag = " --allow-http-registry --allow-insecure-registry"
	}

	const (
		outSignaturePath = "/tmp/cosign-sig.txt"
		outDigestPath    = "/tmp/cosign-digest.txt"
		// Projected SA token path. Build cluster SA must be configured to project
		// a token suitable for OIDC consumption by Fulcio.
		oidcTokenPath = "/var/run/secrets/sigstore/token" //nolint:gosec // G101 false positive: filesystem PATH, not credential value
	)

	fulcioURL := p.FulcioURL
	if fulcioURL == "" {
		fulcioURL = "https://fulcio.sigstore.dev"
	}
	rekorURL := p.RekorURL
	if rekorURL == "" {
		rekorURL = "https://rekor.sigstore.dev"
	}

	script := fmt.Sprintf(`set -e
# Extract image digest from BuildKit metadata.
DIGEST=$(sed -n 's/.*"containerimage.digest":[[:space:]]*"\([^"]*\)".*/\1/p' %s)
if [ -z "$DIGEST" ]; then
  echo "ERROR: no digest found in metadata file" >&2
  exit 1
fi
echo "Keyless-signing image: %s@${DIGEST} (Fulcio: %s, Rekor: %s)"

# Read OIDC identity token for Fulcio. Build cluster SA must be configured to
# project a token suitable for OIDC consumption.
if [ ! -r %s ]; then
  echo "ERROR: SA OIDC token not mounted at %s — keyless mode unavailable" >&2
  exit 1
fi
export SIGSTORE_ID_TOKEN="$(cat %s)"

# Sign keyless. Fulcio issues short-lived cert; Rekor records the entry.
COSIGN_EXPERIMENTAL=1 cosign sign --yes%s \
  --fulcio-url=%s \
  --rekor-url=%s \
  -a "build-system=loopzero" \
  -a "app=%s" \
  -a "mode=keyless" \
  "%s@${DIGEST}"

printf '%%s' "%s@${DIGEST}" > %s
printf '%%s' "${DIGEST}"   > %s

echo "Image signed successfully (keyless)"
`,
		metadataFile,
		p.ImageTag, fulcioURL, rekorURL,
		oidcTokenPath, oidcTokenPath, oidcTokenPath,
		insecureFlag,
		fulcioURL, rekorURL,
		p.AppName,
		p.ImageTag,
		p.ImageTag, outSignaturePath,
		outDigestPath)

	return &forge.Container{
		Name:    fmt.Sprintf("sign-image-%s", p.AppName),
		Image:   loopzeroCosignImage(),
		Command: []string{"sh", "-c", script},
		Outputs: []forge.Parameter{
			{Name: "signature", ValueFrom: &model.ValueFrom{Path: outSignaturePath}},
			{Name: "digest", ValueFrom: &model.ValueFrom{Path: outDigestPath}},
		},
		Env: []forge.EnvBuilder{
			forge.Env{Name: "HOME", Value: "/tmp"},
			forge.Env{Name: "TUF_ROOT", Value: "/tmp/.sigstore/root"},
			forge.Env{Name: "DOCKER_CONFIG", Value: buildkitRegistryMount},
			// COSIGN_EXPERIMENTAL=1 enables keyless flow. Newer cosign versions (v2.2+)
			// have keyless on by default but we keep the env var for compatibility.
			forge.Env{Name: "COSIGN_EXPERIMENTAL", Value: "1"},
		},
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretRegistryConfig, MountPath: buildkitRegistryMount, ReadOnly: true,
			}, SecretName: secretRegistryConfig},
			// NOTE: SA OIDC token is mounted via projected volume defined in the
			// workflow pod spec, not here. The container expects the token at
			// /var/run/secrets/sigstore/token. The pod-level config is added by
			// the workflow_builder when InfraParams.CosignKeyless=true.
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "100m", Memory: "128Mi"},
			Limits:   forge.ResourceList{CPU: "500m", Memory: "256Mi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsUser:    ptr(rootlessUID),
			RunAsGroup:   ptr(rootlessUID),
			RunAsNonRoot: ptr(true),
		},
	}
}
