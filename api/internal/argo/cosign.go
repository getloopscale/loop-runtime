// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"
)

// cosignSignTemplate generates an inline container template that signs a
// container image with cosign after BuildKit pushes it.
//
// Sprint 6: Supply chain security. Every user image is signed at build time.
// The cosign private key is mounted from K8s Secret `cosign-signing-key`.
// Registry credentials are shared with the BuildKit step (same Secret).
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
		Image:   imageCosignSigner,
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
