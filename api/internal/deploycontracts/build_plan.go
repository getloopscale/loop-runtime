// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deploycontracts

import (
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// BuildPlan contains resolved build instructions for all apps in a deploy.
// This is the intermediary between "what the user wants" (AppUnit) and
// "how the pipeline executes" (Workflow CR). The WorkflowBuilder receives
// a BuildPlan — it doesn't need to know about loopzero.yml.
type BuildPlan struct {
	Apps []AppBuildPlan
}

// AppBuildPlan contains resolved build instructions for a single app.
type AppBuildPlan struct {
	AppName          string            // e.g., "api"
	ImageTag         string            // e.g., "registry.do.com/tenant/project/api:sha123"
	DockerfilePath   string            // resolved — absolute in workspace (e.g., "/workspace/apps/api/Dockerfile")
	ContextPath      string            // e.g., "/workspace" or "/workspace/apps/api"
	BuildArgs        map[string]string // from AppUnit.Env + system vars (COMMIT_SHA, APP_NAME)
	CacheRef         string            // OCI cache ref (e.g., "registry.do.com/tenant/project/api:buildcache")
	LanguageStrategy string            // resolved strategy name (e.g., "node", "python", "custom")
	Runtime          string            // services.json runtime: "python" | "node" — the build-strategy key (T0.1, F-dom-2 wiring)
	Build            string            // "dockerfile", "buildpack", "custom"

	// Original AppUnit fields needed downstream (manifests, ArgoCD, Dockerfile generation)
	AppType string // "server", "worker", "frontend"
	AppPath string // original app path from loopzero.yaml (e.g., "apps/api", ".")
	Port    int
	Start   string // T0.2b: declared services.json start command (canonical Dockerfile CMD)
	Secrets []string

	// ReusedDigest is the digest of the signed image an earlier build of the
	// same commit already pushed for this app. When set, the workflow emits no
	// image build, signing, SBOM or attestation for the app: its tag is
	// immutable, so a rebuild could not be pushed.
	ReusedDigest string
}

// CreateBuildPlan resolves AppUnit[] into concrete build instructions.
// Each AppUnit produces one AppBuildPlan with all paths, tags, and args resolved.
func CreateBuildPlan(units []AppUnit, tenantID, projectID, commitSHA, registryURL string) BuildPlan {
	plans := make([]AppBuildPlan, 0, len(units))

	for _, unit := range units {
		// ARCH-001 fix: single source of truth for image tag format
		imageTag := naming.ImageTag(registryURL, tenantID, projectID, unit.Name, commitSHA)

		cacheRef := fmt.Sprintf("%s/%s/%s-%s:buildcache",
			registryURL, tenantID, projectID, unit.Name)

		// Resolve Dockerfile path in workspace.
		// T2.2/D4 (plan simplify-deploy-loopzerokit-only): the BYO user-Dockerfile
		// path is DELETED. The Dockerfile is ALWAYS the platform-canonical file
		// written by the generate-dockerfile step — a user-supplied build mode /
		// dockerfile name / subpath is NEVER honored here. This single-sources the
		// path with argo.dockerfilePathForApp (MEET-2026-03-29-018):
		//   1 app  → /workspace/Dockerfile
		//   N apps → /workspace/Dockerfile.{name}
		// so the generator's write path and BuildKit's `--opt filename` can never
		// diverge (which would otherwise let BuildKit build a user Dockerfile).
		var dockerfilePath string
		if len(units) > 1 {
			dockerfilePath = fmt.Sprintf("/workspace/Dockerfile.%s", unit.Name)
		} else {
			dockerfilePath = "/workspace/Dockerfile"
		}

		// Context is ALWAYS the whole workspace: the canonical Dockerfile does
		// COPY . . then WORKDIR into the app subdir. Never the user subpath.
		contextPath := "/workspace"

		// Build args are the platform's only: COMMIT_SHA and APP_NAME. No user
		// value reaches `buildctl --opt` from here, which is why there is no
		// key-name or reserved-prefix filter at this layer — AppUnit carries no
		// env, and the filter that used to run here iterated a field nothing
		// populated (removed in B-094). A future change that forwards user
		// values into build args must bring its own validation with it.
		buildArgs := make(map[string]string, 2)
		buildArgs["COMMIT_SHA"] = commitSHA
		buildArgs["APP_NAME"] = unit.Name

		// simplify-deploy-loopzerokit-runtime-wiring T2.1 (F-dom-2): strategy is keyed
		// on the services.json `runtime` ONLY (python|node). No legacy framework
		// fallback (D1/D2). An empty runtime is a contract violation and fails
		// closed downstream via argo.ResolveStrategy (D4).
		if unit.Runtime == "" {
			slog.Warn("build plan: missing runtime",
				"app", unit.Name,
				"hint", "services.json must declare runtime (python|node) — build will fail closed")
		}
		strategy := resolveLanguageStrategy(unit.Runtime)

		plans = append(plans, AppBuildPlan{
			AppName:          unit.Name,
			ImageTag:         imageTag,
			DockerfilePath:   dockerfilePath,
			ContextPath:      contextPath,
			BuildArgs:        buildArgs,
			CacheRef:         cacheRef,
			LanguageStrategy: strategy,
			Runtime:          unit.Runtime, // T0.1 (F-dom-2 wiring): carry runtime to workflow strategy resolution
			Build:            unit.Build,
			AppType:          unit.Type,
			AppPath:          unit.Path,
			Port:             unit.Port,
			Start:            unit.Start, // T0.2b: thread declared start to the build plan
			Secrets:          unit.Secrets,
		})
	}

	return BuildPlan{Apps: plans}
}

// resolveLanguageStrategy maps a services.json `runtime` to a language
// strategy label. Single source of truth for the label on AppBuildPlan.
//
// simplify-deploy-loopzerokit-runtime-wiring T2.1 (F-dom-2): keyed on the
// services.json `runtime` only (python|node). The legacy loopzero.yaml framework
// cases were deleted (D1/D2 — no backward compatibility). Anything else returns
// "unsupported"; the actual build fails closed via argo.ResolveStrategy, which
// is the authoritative dispatch (this label is informational on AppBuildPlan).
func resolveLanguageStrategy(runtime string) string {
	switch runtime {
	case "node":
		return "node"
	case "python":
		return "python"
	default:
		return "unsupported"
	}
}
