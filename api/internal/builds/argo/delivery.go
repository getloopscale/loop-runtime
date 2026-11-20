// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
	"github.com/getloopscale/argo-workflows-go/model"
)

// uploadAssetsTemplate generates a container template that uploads static build
// output to S3 (DO Spaces). This replaces the BuildKit template for static apps.
//
// The step:
//  1. Finds the build output dir (dist/, build/, out/, .next/standalone/)
//  2. Generates asset-manifest.json with SHA256 checksums
//  3. Uploads everything to S3: s3://{bucket}/{tenant}/{project}/{app}/{build-id}/
//
// Uses aws cli for S3 operations. Credentials come from the existing
// loopzero-minio-credentials Secret in the build namespace (envFrom in CWT).
func (b *WorkflowBuilder) uploadAssetsTemplate(spec deploycontracts.WorkflowSpec, plan deploycontracts.AppBuildPlan) *forge.Script {
	appPath := plan.AppPath
	if appPath == "" || appPath == "." {
		appPath = "."
	}

	s3Dest := fmt.Sprintf("%s/%s/%s/%s/",
		spec.TenantID, spec.ProjectID, plan.AppName, spec.BuildID)

	// Script: find output dir, generate manifest, upload via aws s3 sync
	script := fmt.Sprintf(`set -e
cd /workspace/%s

# Find build output directory
OUTPUT_DIR=""
for dir in dist build out .next/standalone public; do
  if [ -d "$dir" ]; then
    OUTPUT_DIR="$dir"
    break
  fi
done

if [ -z "$OUTPUT_DIR" ]; then
  echo "ERROR: No build output directory found (checked: dist, build, out, .next/standalone, public)"
  exit 1
fi

echo "Upload source: $OUTPUT_DIR"

# Generate asset-manifest.json with SHA256 checksums
# CODE-003 fix: removed head -100 limit — real builds produce 200-500+ chunk files.
# CODE-006 fix: use proper JSON escaping to handle filenames with special characters.
echo '{"files":[' > /tmp/manifest.json
FIRST=true
find "$OUTPUT_DIR" -maxdepth 1 -type f | while read filepath; do
  f=$(basename "$filepath")
  HASH=$(sha256sum "$filepath" | cut -d' ' -f1)
  if [ "$FIRST" = true ]; then
    FIRST=false
  else
    printf ',' >> /tmp/manifest.json
  fi
  # Escape double quotes in filenames for valid JSON
  SAFE_F=$(echo "$f" | sed 's/"/\\"/g')
  printf '{"path":"%%s","sha256":"%%s"}' "$SAFE_F" "$HASH" >> /tmp/manifest.json
done
echo ']}' >> /tmp/manifest.json
cp /tmp/manifest.json "$OUTPUT_DIR/asset-manifest.json"
echo "Asset manifest:"
cat "$OUTPUT_DIR/asset-manifest.json"

# Upload to S3 via aws cli (MINIO_ENDPOINT used as S3 endpoint)
echo "Uploading to s3://%s/%s"
aws s3 sync "$OUTPUT_DIR/" "s3://%s/%s" --endpoint-url "$MINIO_ENDPOINT" --acl public-read --no-progress
echo "Upload complete"
`, appPath, s3BucketStaticAssets, s3Dest, s3BucketStaticAssets, s3Dest)

	return &forge.Script{
		Name:    fmt.Sprintf("deliver-artifact-%s", plan.AppName),
		Image:   imageAWSCLI,
		Command: []string{"sh"},
		Source:  script,
		Env: []forge.EnvBuilder{
			forge.Env{Name: "HOME", Value: "/tmp"},
		},
		EnvFrom: []model.EnvFromSource{
			{SecretRef: &model.SecretEnvSource{Name: secretMinIOCreds}},
		},
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "100m", Memory: "128Mi"},
			Limits:   forge.ResourceList{CPU: "500m", Memory: "512Mi"},
		},
		RetryStrategy: forge.NewRetryOnFailure(2, "10s", "2", "120s"),
	}
}
