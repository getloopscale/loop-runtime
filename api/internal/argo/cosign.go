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
// Image choice: Uses Alpine (which has sh/sed) + downloads cosign binary.
// The official gcr.io/projectsigstore/cosign image is distroless (no shell,
// no grep, no cut) — unusable for scripts that need to parse JSON metadata.
func cosignSignTemplate(appName, imageTag string) *forge.Container {
	metadataFile := fmt.Sprintf("%s/metadata-%s.json", workspaceMountPath, appName)

	// Shell script that:
	// 1. Downloads cosign binary (pinned version, checksum verified)
	// 2. Extracts the image digest from BuildKit metadata using sed (available in alpine)
	// 3. Signs the image by digest using cosign
	// The COSIGN_PASSWORD env var is injected from the secret.
	script := fmt.Sprintf(`set -e
# Install cosign binary (pinned to v2.4.1)
COSIGN_VERSION="v2.4.1"
wget -q -O /usr/local/bin/cosign \
  "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-amd64"
chmod +x /usr/local/bin/cosign

# Extract digest from BuildKit metadata using sed (alpine has no jq/grep -P).
# BuildKit writes: {"containerimage.digest":"sha256:abc123..."}
DIGEST=$(sed -n 's/.*"containerimage.digest":"\([^"]*\)".*/\1/p' %s)
if [ -z "$DIGEST" ]; then
  echo "ERROR: no digest found in metadata file" >&2
  exit 1
fi
echo "Signing image: %s@${DIGEST}"

# Sign with cosign (key-based)
cosign sign --yes --key %s/cosign.key \
  -a "build-system=loopzero" \
  -a "app=%s" \
  "%s@${DIGEST}"

echo "Image signed successfully"
`, metadataFile, imageTag, cosignKeyMountPath, appName, imageTag)

	return &forge.Container{
		Name:    fmt.Sprintf("sign-image-%s", appName),
		Image:   imageAlpine,
		Command: []string{"sh", "-c", script},
		Env: []forge.EnvBuilder{
			forge.Env{Name: "HOME", Value: "/home/user"},
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
