// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/loopzero/loopzero/pkg/naming"
)

// envKeyRegex validates environment variable key names.
// SEC-002 fix: user-supplied env keys from loopzero.yaml flow into buildctl --opt build-arg:KEY=VALUE.
// Without validation, a crafted key could inject additional buildctl flags.
var envKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnvKeyPrefixes mirrors tools/loopzero-dockerfile-lint/main.go: keys
// with these prefixes are reserved for platform injection and MUST NOT be
// overridable by user-submitted loopzero.yaml env. T2.5 of
// deep-review-remediation-plan (closes find_sec_004 MEDIUM).
var reservedEnvKeyPrefixes = []string{
	"LOOPZERO_",
	"KUBERNETES_",
	"AWS_",
	"DO_",
	"GOOGLE_",
}

// isReservedEnvKey returns true if the key starts with a reserved prefix.
// Comparison is exact-prefix and case-sensitive (matching K8s + cloud SDK
// conventions where these env names are uppercase).
func isReservedEnvKey(k string) bool {
	for _, p := range reservedEnvKeyPrefixes {
		if len(k) >= len(p) && k[:len(p)] == p {
			return true
		}
	}
	return false
}

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
	LanguageStrategy string            // resolved strategy name (e.g., "node", "go", "python", "custom")
	Build            string            // "dockerfile", "buildpack", "custom"

	// Original AppUnit fields needed downstream (manifests, ArgoCD, Dockerfile generation)
	AppType       string // "server", "worker", "frontend"
	AppPath       string // original app path from loopzero.yaml (e.g., "apps/api", ".")
	Port          int
	Start         string // T0.2b: declared services.json start command (canonical Dockerfile CMD)
	Secrets       []string
	DeliveryModel string // "container" or "static" (MEET-009). Resolved from AppUnit.Rendering.
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

		// Build args: user env + system vars
		// SEC-002 fix: validate env key names before they reach buildctl --opt.
		// T2.5 of deep-review-remediation-plan (closes find_sec_004 MEDIUM):
		// also reject reserved-prefix env keys (LOOPZERO_*, KUBERNETES_*, AWS_*,
		// DO_*, GOOGLE_*) at build-plan time so user submissions can't
		// override platform-injected values. loopzero-dockerfile-lint already
		// blocks these in user Dockerfiles (reservedEnvKeyPrefixes); this
		// mirrors the check at the API submission layer for defense in depth.
		buildArgs := make(map[string]string, len(unit.Env)+2)
		for k, v := range unit.Env {
			if !envKeyRegex.MatchString(k) {
				slog.Warn("build plan: skipping env with invalid key name",
					"key", k, "app", unit.Name, "reason", "must match [A-Za-z_][A-Za-z0-9_]*")
				continue
			}
			if isReservedEnvKey(k) {
				slog.Warn("build plan: skipping env with reserved key prefix",
					"key", k, "app", unit.Name, "reason", "reserved by LoopZero platform — cannot be overridden by user")
				continue
			}
			buildArgs[k] = v
		}
		buildArgs["COMMIT_SHA"] = commitSHA
		buildArgs["APP_NAME"] = unit.Name

		// Plan B v3.1 T-B.5 (2026-06-05) — strategy dispatch via services.json
		// `runtime` first, with legacy `framework` fallback (ADR D7 transition
		// window). EC-B3-2: warn when both empty (likely partial migration or
		// data corruption) and fall through to "custom" via the default branch.
		runtimeOrFramework := unit.Runtime
		if runtimeOrFramework == "" {
			runtimeOrFramework = unit.Framework
		}
		if runtimeOrFramework == "" {
			slog.Warn("build plan: E-010 missing runtime+framework",
				"app", unit.Name,
				"hint", "likely partial migration or data corruption — falling back to custom strategy")
		}
		strategy := resolveLanguageStrategy(runtimeOrFramework)

		// T3.3 (simplify-deploy-loopzerokit-only): static-frontend delivery removed —
		// every app is container delivery (AppUnit.Rendering is always "server").
		// The "static" branch here was dead (F-wire-2). DeliveryModel is retained
		// (always "container") for downstream-struct compatibility.
		deliveryModel := "container"
		slog.Info("build plan: resolved delivery model",
			"app", unit.Name, "delivery_model", deliveryModel)

		plans = append(plans, AppBuildPlan{
			AppName:          unit.Name,
			ImageTag:         imageTag,
			DockerfilePath:   dockerfilePath,
			ContextPath:      contextPath,
			BuildArgs:        buildArgs,
			CacheRef:         cacheRef,
			LanguageStrategy: strategy,
			Build:            unit.Build,
			AppType:          unit.Type,
			AppPath:          unit.Path,
			Port:             unit.Port,
			Start:            unit.Start, // T0.2b: thread declared start to the build plan
			Secrets:          unit.Secrets,
			DeliveryModel:    deliveryModel,
		})
	}

	return BuildPlan{Apps: plans}
}

// resolveLanguageStrategy maps a runtime/framework string to a language
// strategy name. Single source of truth for the dispatch.
//
// Plan B v3.1 T-B.5 (2026-06-05) — accepts both the new services.json
// `runtime` values ("python", "node") and the legacy loopzero.yaml `framework`
// values (everything else). EC-B3-3 explicit cases ship at the top so the
// runtime contract is unmistakable in code review; the legacy framework
// cases follow during the transition window per ADR D7.
func resolveLanguageStrategy(runtimeOrFramework string) string {
	switch runtimeOrFramework {
	// services.json runtime values (Plan B v3.1 T-B.5 EC-B3-3).
	case "node":
		return "node"
	case "python":
		return "python"
	// Legacy loopzero.yaml framework values — kept during transition (ADR D7).
	case "nextjs", "next", "vite", "remix", "nestjs", "express",
		"react", "vue", "svelte", "astro", "angular", "fastify":
		return "node"
	case "gin", "go":
		return "go"
	case "fastapi", "django":
		return "python"
	case "sinatra", "rails", "ruby":
		return "ruby"
	case "spring", "spring-boot", "java":
		return "java"
	case "laravel", "slim", "php":
		return "php"
	case "axum", "actix", "rocket", "rust":
		return "rust"
	default:
		return "custom"
	}
}
