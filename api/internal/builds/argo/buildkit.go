// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argo builds the Argo Workflow specs that drive every LoopZero
// build: BuildKit image build, cosign signing, syft SBOM generation,
// language-strategy dispatch, and supply-chain admission. Built atop
// the loopzero-forge SDK and the LanguageStrategy interface
// (`ResolveStrategyForApp`). See CHANGELOG.md "Build Mode Dispatch"
// + finding-2026-05-08-build-mode-dispatch.
package argo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
	"github.com/getloopscale/argo-workflows-go/model"

	"github.com/getloopscale/loopzero/api/internal/builds/shellgen"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// buildkitParams holds the resolved parameters for a BuildKit build container.
// buildkitTemplateFromPlan resolves AppBuildPlan inputs into this common struct,
// which buildkitContainer uses to generate the template. T1.3 deleted the
// legacy buildkitTemplateForApp peer.
type buildkitParams struct {
	AppName        string
	ImageTag       string
	DockerfilePath string
	ContextPath    string
	CacheRef       string
	BuildArgs      map[string]string
	NoCache        bool
}

// buildkitTemplateForApp was the legacy single-arg BuildKit template generator
// that derived imageTag/cacheRef/dockerfile from spec.Apps inline. Deleted in
// T1.3 (CR-2 of MEET-2026-05-28-001) along with all spec.Apps fallback paths
// in WorkflowBuilder. The only emission path now is buildkitTemplateFromPlan,
// which consumes resolved AppBuildPlan fields produced by deploycontracts.CreateBuildPlan.

// buildkitTemplateFromPlan generates a single-container BuildKit template using resolved
// fields from AppBuildPlan. Same architecture as buildkitTemplateForApp (MEET-2026-03-26-007)
// but all paths, tags, and args come from the pre-resolved plan.
func (b *WorkflowBuilder) buildkitTemplateFromPlan(spec deploycontracts.WorkflowSpec, plan deploycontracts.AppBuildPlan) (*forge.Container, error) {
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
func (b *WorkflowBuilder) buildkitContainer(p buildkitParams) (*forge.Container, error) {
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
	metadataFile := metadataFileFor(p.AppName)
	buildctlArgs = append(buildctlArgs, "--metadata-file", metadataFile)

	// Task 3.1: SLSA provenance attestation on user-built images.
	// BuildKit 0.11+ embeds provenance metadata in the image manifest.
	buildctlArgs = append(buildctlArgs, "--opt", "attest:provenance=mode=max")

	if b.infraParams.RegistryInsecure {
		buildctlArgs = append(buildctlArgs, "--allow", "security.insecure")
	}

	entrypoint, err := buildkitEntrypointScript(socketPath, b.infraParams.RegistryInsecure, b.infraParams.RegistryURL, metadataFile)
	if err != nil {
		return nil, err
	}

	return &forge.Container{
		Name:  naming.BuildImageStepName(p.AppName),
		Image: imageBuildKit,
		// rootlesskit sets up user namespace, then runs the entrypoint script.
		// Script starts buildkitd background, waits for socket, runs buildctl "$@".
		// "--" is $0, "buildctl" is $1, rest of buildctlArgs become $2+.
		Command: []string{"rootlesskit", "sh", "-c", entrypoint, "--"},
		Args:    append([]string{"buildctl"}, buildctlArgs...),
		// CHG-2026-04-25-ATTESTATION-WIRE: surface the image digest as a
		// workflow output parameter so the API can persist it on the
		// release row. The entrypoint script writes /tmp/build-digest.txt
		// post-build (defensive — missing file ⇒ empty output, deploy
		// still succeeds).
		Outputs: []forge.Parameter{
			{
				Name:      "digest",
				ValueFrom: &model.ValueFrom{Path: "/tmp/build-digest.txt"},
			},
		},
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
			RunAsUser:    forge.Ptr(rootlessUID),
			RunAsGroup:   forge.Ptr(rootlessUID),
			RunAsNonRoot: forge.Ptr(true),
			// Seccomp Unconfined required for rootlesskit to create user namespaces
			// on managed Kubernetes (DOKS, EKS, GKE).
			SeccompProfile: &model.SeccompProfile{Type: "Unconfined"},
		},
		// AppArmor unconfined on the main container — required for rootless BuildKit
		// mount operations inside user namespaces on managed Kubernetes with AppArmor.
		Annotations: map[string]string{
			"container.apparmor.security.beta.kubernetes.io/main": "unconfined",
		},
		// L3 v8 observed 4 workflows "Max duration limit exceeded" at c=30.
		// BuildKit first-attempt cold build under contention can exceed 300s
		// (image layer pulls + dependency install + build). Bump MaxDuration
		// to 900s (15 min) — happy path remains 2-3min. Budget covers 3 retries
		// with exponential backoff 10s, 20s, 40s.
		RetryStrategy: forge.NewRetryOnFailure(3, "10s", "2", "900s"),
		// Sprint B T3.1 (plan 2026-06-01-p2-interface-contracts-robustness,
		// ADR D3): hard cap at 1800s (30min) aligned with the DB reaper's
		// RunningTimeout (services/build_reaper.go). Without this cap, the
		// retry budget above (3 × 900s ≈ 45min cumulative) outlasts the
		// reaper by 15min — the DB row transitions to FAILED while the
		// workflow keeps running, an orphan-execution window.
		// Builds legitimately requiring >30min must request an SLA bump
		// (future tenant-plan field); for pre-release, 30min is the
		// documented per-build SLA.
		ActiveDeadlineSeconds: forge.Ptr(1800),
	}, nil
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
func buildkitEntrypointScript(socketPath string, insecure bool, registryURL, metadataFile string) (string, error) {
	b := shellgen.New().Raw("set -e")

	quotedSocket := shellgen.ShellQuote(socketPath)
	buildkitdArgs := "--oci-worker-no-process-sandbox --addr " + quotedSocket

	// Validate registryURL before TOML injection — reject characters that
	// could break the TOML structure or inject arbitrary content. It is
	// configuration, so a bad value is an error for the caller, not a panic.
	if i := strings.IndexAny(registryURL, "\"\n\r\\"); i >= 0 {
		return "", fmt.Errorf("buildkit: registry URL %q contains invalid character %q", registryURL, registryURL[i])
	}

	// Always configure buildkitd.toml with a docker.io mirror (mirror.gcr.io) to
	// avoid Docker Hub anonymous rate limits (100 pulls/6h) during L2+ load tests.
	// mirror.gcr.io is Google's public Docker Hub mirror, no auth needed.
	// See https://cloud.google.com/container-registry/docs/pulling-cached-images.
	var tomlBuilder strings.Builder
	tomlBuilder.WriteString(`[registry."docker.io"]
  mirrors = ["mirror.gcr.io"]
`)
	if insecure {
		buildkitdArgs += " --oci-worker-snapshotter native"
		buildkitdArgs += " --allow-insecure-entitlement security.insecure"
		fmt.Fprintf(&tomlBuilder, `[registry."%s"]
  http = true
  insecure = true
`, registryURL)
	}

	b.Blank().
		Comment("Configure buildkitd.toml (docker.io mirror to avoid rate limits)").
		MkdirP("/tmp/buildkit").
		Raw(fmt.Sprintf(`cat > /tmp/buildkit/buildkitd.toml <<'TOML'
%sTOML`, tomlBuilder.String()))
	buildkitdArgs += " --config /tmp/buildkit/buildkitd.toml"

	b.Blank().
		Comment("Start buildkitd daemon in background (inside rootlesskit user namespace)").
		Raw(fmt.Sprintf("buildkitd %s &", buildkitdArgs)).
		Raw("DAEMON_PID=$!").
		Blank().
		Comment("Wait for daemon to accept gRPC connections").
		Raw(fmt.Sprintf("timeout=%d; i=0", buildkitDaemonTimeout)).
		Raw(fmt.Sprintf("while ! buildctl --addr %s debug workers >/dev/null 2>&1; do", quotedSocket)).
		Raw(`  i=$((i+1))`).
		Raw(`  if [ "$i" -ge "$timeout" ]; then`).
		Raw(`    echo "buildkitd not ready after ${timeout}s" >&2`).
		Raw(`    kill $DAEMON_PID 2>/dev/null || true`).
		Raw(`    exit 1`).
		Raw(`  fi`).
		Raw(`  sleep 1`).
		Raw(`done`).
		Blank().
		Comment("Run the build — \"$@\" preserves all args from Argo container spec.").
		Comment("Capture exit code BEFORE killing daemon.").
		Raw(`"$@"`).
		Raw(`BUILD_EXIT=$?`).
		Blank().
		Comment("CHG-2026-04-25-ATTESTATION-WIRE: extract THIS app's image digest").
		Comment("from its own BuildKit metadata file (the workspace is shared by").
		Comment("every app's build) and write it to the workflow output path.").
		Comment("Defensive: missing/malformed metadata MUST NOT fail the").
		Comment("build — the digest is observability data; the deploy is").
		Comment("the source of truth.").
		Raw(fmt.Sprintf(`if [ "$BUILD_EXIT" -eq 0 ] && [ -f %s ]; then`, shellgen.ShellQuote(metadataFile))).
		Raw(fmt.Sprintf(`  %s > /tmp/build-digest.txt 2>/dev/null || true`, digestFromMetadata(metadataFile))).
		Raw(`fi`).
		Blank().
		Comment("Graceful daemon shutdown — exit code is already captured").
		Raw(`kill $DAEMON_PID 2>/dev/null || true`).
		Raw(`wait $DAEMON_PID 2>/dev/null || true`).
		Raw(`exit $BUILD_EXIT`)

	return b.String(), nil
}

// ---------------------------------------------------------------------------
// Digest extraction
// ---------------------------------------------------------------------------

// metadataFileFor is the BuildKit --metadata-file of one app's build on the
// shared workspace. Every step that needs the app's image digest reads THIS
// file: the workspace holds one per app.
func metadataFileFor(appName string) string {
	return fmt.Sprintf("%s/metadata-%s.json", workspaceMountPath, appName)
}

// digestFromMetadata is the shell command that prints the image digest
// recorded in a BuildKit metadata file ({"containerimage.digest":"sha256:..."}).
func digestFromMetadata(metadataFile string) string {
	return `sed -n 's/.*"containerimage.digest":[[:space:]]*"\([^"]*\)".*/\1/p' ` + shellgen.ShellQuote(metadataFile)
}
