// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"
)

// SBOM artifact constants — single source of truth for paths/format.
const (
	sbomLocalDir       = "/tmp"
	sbomFormatCDXJSON  = "cyclonedx-json"
	sbomS3PathTemplate = "sbom/%s/%s/%s/%s.cdx.json" // tenant/project/commit/app
)

// syftScanTemplate generates an inline container template that scans a
// signed image and produces a CycloneDX JSON SBOM.
//
// Sprint 6 / T1.1 — Supply chain SBOM. The SBOM step runs AFTER cosign
// signing (so the digest is stable and signed) and BEFORE harbor mirror
// (so the SBOM is generated for the canonical image).
//
// Trust model:
//   - The container reads the image digest from BuildKit's metadata file
//     (same path cosign uses) — this is the cryptographic digest.
//   - Syft scans by digest, NOT by tag, so the SBOM is pinned to the
//     exact image content cosign signed.
//   - Outputs are written to the container's OWN /tmp filesystem; they
//     are not shared with other containers in the pod.
//
// SEC-006: Uses pre-built loopzero-syft-scanner image with syft baked in.
// No runtime binary downloads.
//
// CHG-2026-04-22-001 (B7): RegistryInsecure mirrors BuildKit's flag for
// local k3d. Production registries (DOCR/Harbor) are HTTPS.
//
// continueOn.failed: managed at the DAG level, not template level — see
// workflow_builder.go where the sbom-{app} task is added with
// ContinueOn{Failed: true}. SBOM failure must NOT block the deploy.
func syftScanTemplate(
	tenantID, projectID, commitSHA, appName, imageTag string,
	insecureRegistry bool,
) *forge.Container {
	metadataFile := fmt.Sprintf("%s/metadata-%s.json", workspaceMountPath, appName)
	sbomLocalPath := fmt.Sprintf("%s/sbom-%s.cdx.json", sbomLocalDir, appName)
	s3Key := fmt.Sprintf(sbomS3PathTemplate, tenantID, projectID, commitSHA, appName)

	const (
		outS3KeyPath          = "/tmp/sbom-s3-key.txt"
		outFormatPath         = "/tmp/sbom-format.txt"
		outComponentCountPath = "/tmp/sbom-component-count.txt"
	)

	insecureFlag := ""
	if insecureRegistry {
		insecureFlag = " --source-registry-insecure --source-registry-token=insecure"
	}

	// Script:
	// 1. Extract digest from BuildKit metadata.
	// 2. Run syft against registry:{tag}@{digest} → CycloneDX JSON.
	// 3. Count components (top-level "components" array length).
	// 4. Upload to S3 (MinIO/DO Spaces) using aws-cli already in image.
	// 5. Write Argo workflow output files.
	//
	// Bucket: loopzero-static-assets (s3BucketStaticAssets) — same bucket
	// as static apps; SBOMs use the sbom/ prefix to avoid collision.
	//
	// AWS_ENDPOINT_URL is set via env (workflow level) for MinIO local
	// dev; in prod the default DO Spaces endpoint is used.
	script := fmt.Sprintf(`set -e
# Extract digest from BuildKit metadata.
DIGEST=$(sed -n 's/.*"containerimage.digest":[[:space:]]*"\([^"]*\)".*/\1/p' %s)
if [ -z "$DIGEST" ]; then
  echo "ERROR: no digest found in metadata file" >&2
  exit 1
fi
echo "Generating SBOM for image: %s@${DIGEST}"

# Scan image and produce CycloneDX JSON. Pin by digest (signed bytes).
syft scan registry:%s@${DIGEST}%s -o %s=%s
if [ ! -s %s ]; then
  echo "ERROR: syft produced no output" >&2
  exit 1
fi

# Component count (CycloneDX: top-level "components" array).
# Robust to absence: default to 0 if the field is missing or non-array.
COMPONENT_COUNT=$(awk -F'"components"' 'NR>1 || /"components"/' %s 2>/dev/null \
  | tr -cd '{' | wc -c)
if [ -z "$COMPONENT_COUNT" ]; then COMPONENT_COUNT=0; fi

# Upload to S3 (bucket: %s). Path: %s
aws s3 cp %s s3://%s/%s

# Persist workflow outputs.
printf '%%s' "%s"             > %s
printf '%%s' "%s"             > %s
printf '%%s' "${COMPONENT_COUNT}" > %s

echo "SBOM generated and uploaded successfully"
`,
		metadataFile,             // 1: metadata path
		imageTag,                 // 2: image tag for echo
		imageTag, insecureFlag,   // 3,4: scan target + insecure flag
		sbomFormatCDXJSON, sbomLocalPath, // 5,6: format + local path
		sbomLocalPath,            // 7: existence check path
		sbomLocalPath,            // 8: count source path
		s3BucketStaticAssets, s3Key, // 9,10: bucket + key (echo)
		sbomLocalPath, s3BucketStaticAssets, s3Key, // 11,12,13: aws s3 cp
		s3Key, outS3KeyPath, // 14,15: write s3 key output
		sbomFormatCDXJSON, outFormatPath, // 16,17: write format output
		outComponentCountPath, // 18: write count output
	)

	return &forge.Container{
		Name:    fmt.Sprintf("sbom-%s", appName),
		Image:   imageSyftScanner,
		Command: []string{"sh", "-c", script},
		Outputs: []forge.Parameter{
			{
				Name:      "sbom_s3_key",
				ValueFrom: &model.ValueFrom{Path: outS3KeyPath},
			},
			{
				Name:      "sbom_format",
				ValueFrom: &model.ValueFrom{Path: outFormatPath},
			},
			{
				Name:      "sbom_component_count",
				ValueFrom: &model.ValueFrom{Path: outComponentCountPath},
			},
		},
		Env: []forge.EnvBuilder{
			// MinIO (local dev) credentials. In prod, AWS_ACCESS_KEY/SECRET
			// for DO Spaces are also injected via the same Secret.
			forge.SecretEnv{Name: "AWS_ACCESS_KEY_ID", SecretName: secretMinIOCreds, SecretKey: "AWS_ACCESS_KEY_ID"},
			forge.SecretEnv{Name: "AWS_SECRET_ACCESS_KEY", SecretName: secretMinIOCreds, SecretKey: "AWS_SECRET_ACCESS_KEY"},
			forge.SecretEnv{Name: "AWS_DEFAULT_REGION", SecretName: secretMinIOCreds, SecretKey: "AWS_DEFAULT_REGION"},
			forge.SecretEnv{Name: "AWS_ENDPOINT_URL", SecretName: secretMinIOCreds, SecretKey: "AWS_ENDPOINT_URL"},
			// Docker config — syft needs this to read from private registries.
			forge.Env{Name: "DOCKER_CONFIG", Value: buildkitRegistryMount},
		},
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretRegistryConfig, MountPath: buildkitRegistryMount, ReadOnly: true,
			}, SecretName: secretRegistryConfig},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "100m", Memory: "256Mi"},
			Limits:   forge.ResourceList{CPU: "500m", Memory: "1Gi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsUser:    ptr(rootlessUID),
			RunAsGroup:   ptr(rootlessUID),
			RunAsNonRoot: ptr(true),
		},
	}
}
