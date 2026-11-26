// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"fmt"

	forge "github.com/getloopscale/argo-workflows-go"
	"github.com/getloopscale/argo-workflows-go/model"

	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// SBOM artifact constants — single source of truth for paths/format.
const (
	// sbomLocalDir MUST be the shared workspace PVC mount path so subsequent
	// DAG steps (cosign-attest) can read the SBOM file. Each step runs in
	// its OWN pod; /tmp is pod-local, /workspace is the shared PVC mount
	// (per workflow_builder.go VolumeClaimTemplates).
	// Discovered LIVE 2026-05-27 (M2 Phase 9 plan 2026-05-27-M07-to-3-85):
	// cosign-attest pod failed with "ERROR: SBOM file not found at
	// /tmp/sbom-X.cdx.json (syft step did not produce output)" — SBOM was
	// in /tmp of syft pod (gone after pod cleanup), not in shared PVC.
	sbomLocalDir       = workspaceMountPath
	sbomFormatCDXJSON  = "cyclonedx-json"
	sbomS3PathTemplate = "sbom/%s/%s/%s/%s.cdx.json" // tenant/project/commit/app
)

// syftScanTemplate generates an inline container template that scans a
// signed image and produces a CycloneDX JSON SBOM.
//
// Sprint 6 / T1.1 — Supply chain SBOM. The SBOM step runs AFTER cosign
// signing, so the digest is stable and signed.
//
// Trust model:
//   - The container reads the image digest from BuildKit's metadata file
//     (same path cosign uses) — this is the cryptographic digest.
//   - Syft scans by digest, NOT by tag, so the SBOM is pinned to the
//     exact image content cosign signed.
//   - Outputs are written to the container's OWN /tmp filesystem; they
//     are not shared with other containers in the pod.
//
// SEC-006: Uses pre-built loopruntime-syft-scanner image with syft baked in.
// No runtime binary downloads.
//
// CHG-2026-04-22-001 (B7): RegistryInsecure mirrors BuildKit's flag for
// local k3d. Production registries are HTTPS.
//
// continueOn.failed: managed at the DAG level, not template level — see
// workflow_builder.go where the sbom-{app} task is added with
// ContinueOn{Failed: true}. SBOM failure must NOT block the deploy.
func syftScanTemplate(
	tenantID, projectID, commitSHA, appName, imageTag string,
	insecureRegistry bool, scannerImage string, store objectStore,
) *forge.Script {
	metadataFile := metadataFileFor(appName)
	sbomLocalPath := fmt.Sprintf("%s/sbom-%s.cdx.json", sbomLocalDir, appName)
	s3Key := fmt.Sprintf(sbomS3PathTemplate, tenantID, projectID, commitSHA, appName)

	const (
		outS3KeyPath          = "/tmp/sbom-s3-key.txt"
		outFormatPath         = "/tmp/sbom-format.txt"
		outComponentCountPath = "/tmp/sbom-component-count.txt"
	)

	// Plan T6.1 dogfood unblock: syft v1.x removed `--source-registry-insecure`
	// CLI flag in favor of env vars `SYFT_REGISTRY_INSECURE_USE_HTTP` (HTTP
	// registry) and `SYFT_REGISTRY_INSECURE_SKIP_TLS_VERIFY` (self-signed TLS).
	// Old flag broke local k3d builds (`unknown flag` error). Env vars are
	// emitted into the script when registry is local-dev (insecure).
	// LOCAL ONLY (LP-001): TLS divergence — local registry is HTTP-only.
	insecureFlag := ""
	insecureEnv := ""
	if insecureRegistry {
		insecureEnv = "export SYFT_REGISTRY_INSECURE_USE_HTTP=true SYFT_REGISTRY_INSECURE_SKIP_TLS_VERIFY=true; "
	}
	_ = insecureFlag // kept as a marker for future syft flag additions; current syft v1.x uses env vars only

	// Script:
	// 1. Extract digest from BuildKit metadata.
	// 2. Run syft against registry:{tag}@{digest} → CycloneDX JSON.
	// 3. Count components (top-level "components" array length).
	// 4. Upload to S3 (MinIO/DO Spaces) using aws-cli already in image.
	// 5. Write Argo workflow output files.
	//
	// Bucket: loopruntime-static-assets (s3BucketStaticAssets) by default, or the
	// configured StaticAssetsBucket; SBOMs live under the sbom/ prefix.
	//
	// AWS_ENDPOINT_URL is set via env (workflow level) for MinIO local
	// dev; in prod the default DO Spaces endpoint is used.
	script := fmt.Sprintf(`set -e
# Extract digest from BuildKit metadata.
DIGEST=$(%s)
if [ -z "$DIGEST" ]; then
  echo "ERROR: no digest found in metadata file" >&2
  exit 1
fi
echo "Generating SBOM for image: %s@${DIGEST}"

# Set syft v1.x env vars for HTTP/insecure registries (Plan T6.1 dogfood unblock).
%s

# Scan image and produce CycloneDX JSON. Pin by digest (signed bytes).
syft scan registry:%s@${DIGEST} -o %s=%s
if [ ! -s %s ]; then
  echo "ERROR: syft produced no output" >&2
  exit 1
fi

# Component count: length of the top-level CycloneDX "components" array.
# python3 ships with aws-cli in the scanner image; jq does not. 0 when the
# field is missing, is not an array, or the file does not parse.
COMPONENT_COUNT=$(python3 -c 'import json,sys; c=json.load(open(sys.argv[1])).get("components"); print(len(c) if isinstance(c, list) else 0)' %s 2>/dev/null || echo 0)
if [ -z "$COMPONENT_COUNT" ]; then COMPONENT_COUNT=0; fi

# Upload to S3 (bucket: %s). Path: %s
aws s3 cp %s s3://%s/%s

# Persist workflow outputs.
printf '%%s' "%s"             > %s
printf '%%s' "%s"             > %s
printf '%%s' "${COMPONENT_COUNT}" > %s

echo "SBOM generated and uploaded successfully"
`,
		digestFromMetadata(metadataFile), // 1: digest extraction from the metadata file
		imageTag,                         // 2: image tag for echo
		insecureEnv,                      // 3: syft env vars for insecure registry (Plan T6.1)
		imageTag,                         // 4: scan target
		sbomFormatCDXJSON, sbomLocalPath, // 5,6: format + local path
		sbomLocalPath,                     // 7: existence check path
		sbomLocalPath,                     // 8: count source path
		store.staticAssetsBucket(), s3Key, // 9,10: bucket + key (echo)
		sbomLocalPath, store.staticAssetsBucket(), s3Key, // 11,12,13: aws s3 cp
		s3Key, outS3KeyPath, // 14,15: write s3 key output
		sbomFormatCDXJSON, outFormatPath, // 16,17: write format output
		outComponentCountPath, // 18: write count output
	)

	return &forge.Script{
		Name:    naming.SBOMStepName(appName),
		Image:   orImage(scannerImage, imageSyftScanner),
		Command: []string{"sh"},
		Source:  script,
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
		Env: syftEnv(store),
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretRegistryConfig, MountPath: buildkitRegistryMount, ReadOnly: true,
			}, SecretName: secretRegistryConfig},
		},
		// LIVE-caught 2026-06-18 against build-25d1977102ec378b: syft
		// scanning a single-app loopzerokit Node.js image OOMKilled at 1Gi
		// limit (exit code 137). syft holds the layer index + dep graph
		// proportional to image size — typical loopzerokit Node.js image is
		// ~300-500MB compressed → ~1.5GB resident at peak. 2Gi limit /
		// 512Mi request gives headroom without bloating cluster scheduling.
		// Future v1.1: tune per-app via loopzero.yaml resource hints.
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "100m", Memory: "512Mi"},
			Limits:   forge.ResourceList{CPU: "500m", Memory: "2Gi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsUser:    forge.Ptr(rootlessUID),
			RunAsGroup:   forge.Ptr(rootlessUID),
			RunAsNonRoot: forge.Ptr(true),
		},
	}
}

// syftEnv is the SBOM step's environment. With IRSA the aws CLI signs as the pod's
// role and reads its region and endpoint from what EKS injects; otherwise the
// credentials, region and endpoint come from the MinIO Secret.
func syftEnv(store objectStore) []forge.EnvBuilder {
	// Docker config: syft reads private registries with it.
	env := []forge.EnvBuilder{forge.Env{Name: "DOCKER_CONFIG", Value: buildkitRegistryMount}}
	if store.IRSA {
		return env
	}
	return append([]forge.EnvBuilder{
		forge.SecretEnv{Name: "AWS_ACCESS_KEY_ID", SecretName: secretMinIOCreds, SecretKey: "AWS_ACCESS_KEY_ID"},         //nolint:gosec // G101 false positive: env var KEY name, not credential value
		forge.SecretEnv{Name: "AWS_SECRET_ACCESS_KEY", SecretName: secretMinIOCreds, SecretKey: "AWS_SECRET_ACCESS_KEY"}, //nolint:gosec // G101 false positive: env var KEY name, not credential value
		forge.SecretEnv{Name: "AWS_DEFAULT_REGION", SecretName: secretMinIOCreds, SecretKey: "AWS_DEFAULT_REGION"},       //nolint:gosec // G101 false positive: env var KEY name, not credential value
		forge.SecretEnv{Name: "AWS_ENDPOINT_URL", SecretName: secretMinIOCreds, SecretKey: "AWS_ENDPOINT_URL"},           //nolint:gosec // G101 false positive: env var KEY name, not credential value
	}, env...)
}
