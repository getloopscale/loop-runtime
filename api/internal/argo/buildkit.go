// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"

	"github.com/loopzero/loopzero/api/internal/services"
)

// buildkitParams holds the resolved parameters for a BuildKit build container.
// Both buildkitTemplateForApp and buildkitTemplateFromPlan resolve their inputs
// into this common struct, which buildkitContainer uses to generate the template.
type buildkitParams struct {
	AppName        string
	ImageTag       string
	DockerfilePath string
	ContextPath    string
	CacheRef       string
	BuildArgs      map[string]string // nil for legacy (no build args)
	NoCache        bool
}

// buildkitTemplateForApp generates a single-container BuildKit template for a single app.
//
// Architecture (MEET-2026-03-26-007): Single container runs both buildkitd (background daemon)
// and buildctl (foreground client) via rootlesskit. This eliminates the sidecar pattern and
// its associated false-failure problem (sidecar exit code 1 on SIGTERM causing Argo to mark
// successful builds as Failed).
//
// Security: rootlesskit requires seccompProfile: Unconfined + AppArmor: unconfined for user
// namespace creation on managed Kubernetes (DOKS, EKS, GKE). buildctl args are not
// user-injectable (constructed programmatically). See MEET-2026-03-26-007 security review.
func (b *WorkflowBuilder) buildkitTemplateForApp(spec services.WorkflowSpec, app services.AppSpec) *forge.Container {
	// Image path: {registry}/{tenant}/{project}-{app}:{sha}
	// Flattened to 2 levels to stay within DO Container Registry nesting limit.
	// Previously 3 levels ({tenant}/{project}/{app}) caused 403 on DO.
	destination := fmt.Sprintf("%s/%s/%s-%s:%s",
		b.infraParams.RegistryURL, spec.TenantID, spec.ProjectID, app.Name, spec.CommitSHA)
	cacheRef := fmt.Sprintf("%s/%s/%s-%s:buildcache",
		b.infraParams.RegistryURL, spec.TenantID, spec.ProjectID, app.Name)

	// Per-app Dockerfile naming — single source of truth (MEET-2026-03-29-018).
	dockerfile := dockerfilePathForApp(app.Name, len(spec.Apps))

	return b.buildkitContainer(buildkitParams{
		AppName:        app.Name,
		ImageTag:       destination,
		DockerfilePath: dockerfile,
		ContextPath:    workspaceMountPath,
		CacheRef:       cacheRef,
		NoCache:        spec.NoCache,
	})
}

// buildkitTemplateFromPlan generates a single-container BuildKit template using resolved
// fields from AppBuildPlan. Same architecture as buildkitTemplateForApp (MEET-2026-03-26-007)
// but all paths, tags, and args come from the pre-resolved plan.
func (b *WorkflowBuilder) buildkitTemplateFromPlan(spec services.WorkflowSpec, plan services.AppBuildPlan) *forge.Container {
	return b.buildkitContainer(buildkitParams{
		AppName:        plan.AppName,
		ImageTag:       plan.ImageTag,
		DockerfilePath: plan.DockerfilePath,
		ContextPath:    plan.ContextPath,
		CacheRef:       plan.CacheRef,
		BuildArgs:      plan.BuildArgs,
		NoCache:        spec.NoCache,
	})
}

// buildkitContainer creates the common BuildKit container template from resolved params.
//
// Single container pattern (MEET-2026-03-26-007): buildkitd and buildctl run in the same
// container via rootlesskit. One container = one exit code. No false failures from sidecar.
//
// Cache: --import-cache and --export-cache use registry-based OCI cache.
// Image digest: extracted from --metadata-file (JSON: {"containerimage.digest":"sha256:..."}).
func (b *WorkflowBuilder) buildkitContainer(p buildkitParams) *forge.Container {
	socketPath := buildkitSocketPath

	buildctlArgs := []string{
		"--addr", socketPath,
		"build",
		"--frontend", "dockerfile.v0",
		"--local", fmt.Sprintf("context=%s", p.ContextPath),
		"--local", fmt.Sprintf("dockerfile=%s", dockerfileDir(p.DockerfilePath)),
		"--opt", fmt.Sprintf("filename=%s", dockerfileName(p.DockerfilePath)),
		"--output", fmt.Sprintf("type=image,name=%s,push=true", p.ImageTag),
	}
	if !p.NoCache {
		buildctlArgs = append(buildctlArgs,
			"--export-cache", fmt.Sprintf("type=registry,ref=%s,mode=max,ignore-error=true", p.CacheRef),
			"--import-cache", fmt.Sprintf("type=registry,ref=%s", p.CacheRef),
		)
	}
	// Sort build arg keys for deterministic buildctl args across runs.
	if len(p.BuildArgs) > 0 {
		buildArgKeys := make([]string, 0, len(p.BuildArgs))
		for k := range p.BuildArgs {
			buildArgKeys = append(buildArgKeys, k)
		}
		sort.Strings(buildArgKeys)
		for _, key := range buildArgKeys {
			buildctlArgs = append(buildctlArgs, "--opt", fmt.Sprintf("build-arg:%s=%s", key, p.BuildArgs[key]))
		}
	}
	buildctlArgs = append(buildctlArgs, "--metadata-file", fmt.Sprintf("%s/metadata-%s.json", workspaceMountPath, p.AppName))

	if b.infraParams.RegistryInsecure {
		buildctlArgs = append(buildctlArgs, "--allow", "security.insecure")
	}

	// seccomp Unconfined on the main container (Argo names it "main" in the pod).
	// Required for rootlesskit to create user namespaces on managed Kubernetes.
	podSpecPatch := `{"containers":[{"name":"main","securityContext":{"seccompProfile":{"type":"Unconfined"}}}]}`

	return &forge.Container{
		Name:  fmt.Sprintf("build-image-%s", p.AppName),
		Image: imageBuildKit,
		// rootlesskit sets up user namespace, then runs the entrypoint script.
		// Script starts buildkitd background, waits for socket, runs buildctl "$@".
		// "--" is $0, "buildctl" is $1, rest of buildctlArgs become $2+.
		Command: []string{"rootlesskit", "sh", "-c", buildkitEntrypointScript(socketPath, b.infraParams.RegistryInsecure, b.infraParams.RegistryURL), "--"},
		Args:    append([]string{"buildctl"}, buildctlArgs...),
		Env: []forge.EnvBuilder{
			forge.Env{Name: "HOME", Value: "/home/user"},
		},
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretRegistryConfig, MountPath: buildkitRegistryMount, ReadOnly: true,
			}, SecretName: secretRegistryConfig},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "500m", Memory: "512Mi"},
			Limits:   forge.ResourceList{CPU: "2000m", Memory: "4Gi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsUser:    ptr(rootlessUID),
			RunAsGroup:   ptr(rootlessUID),
			RunAsNonRoot: ptr(true),
		},
		// AppArmor unconfined on the main container — required for rootless BuildKit
		// mount operations inside user namespaces on managed Kubernetes with AppArmor.
		Annotations: map[string]string{
			"container.apparmor.security.beta.kubernetes.io/main": "unconfined",
		},
		PodSpecPatch:  podSpecPatch,
		RetryStrategy: retryStrategy(3, "10s", "2", "300s"),
	}
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// dockerfilePathForApp returns the canonical Dockerfile path for an app.
// Single source of truth for naming (MEET-2026-03-29-018):
//   - 1 total app  → /workspace/Dockerfile
//   - 2+ total apps → /workspace/Dockerfile.{appName}
//
// Used by: loopzeroPacksDockerfileStep (generator), buildkitTemplateForApp (consumer),
// and CreateBuildPlan (plan consumer). All 3 must agree on the path.
func dockerfilePathForApp(appName string, totalApps int) string {
	if totalApps <= 1 {
		return workspaceMountPath + "/Dockerfile"
	}
	return fmt.Sprintf("%s/Dockerfile.%s", workspaceMountPath, appName)
}

// dockerfileDir returns the directory containing the Dockerfile.
func dockerfileDir(dockerfilePath string) string {
	return filepath.Dir(dockerfilePath)
}

// dockerfileName returns the filename portion of the Dockerfile path.
func dockerfileName(dockerfilePath string) string {
	return filepath.Base(dockerfilePath)
}

// ---------------------------------------------------------------------------
// BuildKit readiness
// ---------------------------------------------------------------------------

// buildkitEntrypointScript returns a shell script that starts buildkitd in
// the background, waits for it to be ready, then runs buildctl with the
// provided arguments.
//
// Single container pattern (MEET-2026-03-26-007): buildkitd and buildctl run
// in the same container, eliminating the sidecar pattern. One container = one
// exit code. No false failures from sidecar SIGTERM.
//
// Lifecycle:
//  1. Start buildkitd in background
//  2. Wait for socket via `buildctl debug workers` (gRPC health check)
//  3. Run buildctl "$@" and capture exit code
//  4. Kill buildkitd on exit (trap ensures cleanup even on failure)
//  5. Exit with buildctl's exit code
func buildkitEntrypointScript(socketPath string, insecure bool, registryURL string) string {
	buildkitdArgs := fmt.Sprintf("--oci-worker-no-process-sandbox --addr '%s'", socketPath)
	configSetup := ""
	if insecure {
		buildkitdArgs += " --oci-worker-snapshotter native"
		buildkitdArgs += " --allow-insecure-entitlement security.insecure"
		// Configure buildkitd to use HTTP for the insecure registry.
		// Without this, buildkitd attempts HTTPS and fails with
		// "http: server gave HTTP response to HTTPS client".
		configSetup = fmt.Sprintf(`
# Configure insecure registry (HTTP) for local dev
mkdir -p /tmp/buildkit
cat > /tmp/buildkit/buildkitd.toml <<TOML
[registry."%s"]
  http = true
  insecure = true
TOML
`, registryURL)
		buildkitdArgs += " --config /tmp/buildkit/buildkitd.toml"
	}
	return fmt.Sprintf(`set -e
%s
# Start buildkitd daemon in background (inside rootlesskit user namespace)
buildkitd %s &
DAEMON_PID=$!`, configSetup, buildkitdArgs) + fmt.Sprintf(`

# Wait for daemon to accept gRPC connections
timeout=%d; i=0
while ! buildctl --addr '%s' debug workers >/dev/null 2>&1; do
  i=$((i+1))
  if [ "$i" -ge "$timeout" ]; then
    echo "buildkitd not ready after ${timeout}s" >&2
    kill $DAEMON_PID 2>/dev/null || true
    exit 1
  fi
  sleep 1
done

# Run the build — "$@" preserves all args from Argo container spec.
# Capture exit code BEFORE killing daemon, so the container exits
# with buildctl's code (the real build result), not buildkitd's.
"$@"
BUILD_EXIT=$?

# Graceful daemon shutdown — exit code is already captured
kill $DAEMON_PID 2>/dev/null || true
wait $DAEMON_PID 2>/dev/null || true
exit $BUILD_EXIT`, buildkitDaemonTimeout, socketPath)
}

// ---------------------------------------------------------------------------
// Digest parsing
// ---------------------------------------------------------------------------

// ParseBuildKitDigest extracts the container image digest from BuildKit's --metadata-file output.
// BuildKit writes JSON like: {"containerimage.digest":"sha256:abc123..."}.
// The digest MUST start with "sha256:" — any other format is treated as a build error.
// Parsing failure must fail the build (deterministic digest extraction is critical for GitOps).
func ParseBuildKitDigest(data []byte) (string, error) {
	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("parse buildkit metadata: invalid JSON: %w", err)
	}
	digestVal, ok := metadata["containerimage.digest"]
	if !ok {
		return "", fmt.Errorf("parse buildkit metadata: missing field 'containerimage.digest'")
	}
	digest, ok := digestVal.(string)
	if !ok || digest == "" {
		return "", fmt.Errorf("parse buildkit metadata: 'containerimage.digest' is empty or not a string")
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("parse buildkit metadata: digest %q does not start with 'sha256:'", digest)
	}
	return digest, nil
}
