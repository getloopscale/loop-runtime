// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"fmt"
	"log/slog"
	"path"
	"regexp"

	"github.com/loopzero/loopzero/pkg/naming"
)

// envKeyRegex validates environment variable key names.
// SEC-002 fix: user-supplied env keys from loopzero.yaml flow into buildctl --opt build-arg:KEY=VALUE.
// Without validation, a crafted key could inject additional buildctl flags.
var envKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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

		// Resolve Dockerfile path in workspace
		var dockerfilePath string
		if unit.Build == "dockerfile" && unit.Dockerfile != "" {
			// User-provided Dockerfile: relative to app path
			dockerfilePath = path.Join("/workspace", unit.Path, unit.Dockerfile)
		} else {
			// Buildpack-generated: the generate-dockerfile step creates it.
			// Naming convention must match argo.dockerfilePathForApp() (MEET-2026-03-29-018).
			if len(units) > 1 {
				dockerfilePath = fmt.Sprintf("/workspace/Dockerfile.%s", unit.Name)
			} else {
				dockerfilePath = "/workspace/Dockerfile"
			}
		}

		// Resolve context path.
		// For buildpack-generated Dockerfiles: context is always /workspace (entire workspace)
		// because the Dockerfile does COPY . . then WORKDIR /app/{appPath}.
		// For user-provided Dockerfiles: context is the app's subdir.
		contextPath := "/workspace"
		if unit.Build == "dockerfile" && unit.Path != "." && unit.Path != "" {
			contextPath = path.Join("/workspace", unit.Path)
		}

		// Build args: user env + system vars
		// SEC-002 fix: validate env key names before they reach buildctl --opt.
		buildArgs := make(map[string]string, len(unit.Env)+2)
		for k, v := range unit.Env {
			if !envKeyRegex.MatchString(k) {
				slog.Warn("build plan: skipping env with invalid key name",
					"key", k, "app", unit.Name, "reason", "must match [A-Za-z_][A-Za-z0-9_]*")
				continue
			}
			buildArgs[k] = v
		}
		buildArgs["COMMIT_SHA"] = commitSHA
		buildArgs["APP_NAME"] = unit.Name

		// Resolve language strategy from framework
		strategy := resolveLanguageStrategy(unit.Framework)

		// Resolve delivery model from rendering (MEET-009)
		deliveryModel := "container"
		if unit.Rendering == "static" {
			deliveryModel = "static"
		}
		slog.Info("build plan: resolved delivery model",
			"app", unit.Name, "rendering", unit.Rendering, "delivery_model", deliveryModel)

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
			Secrets:          unit.Secrets,
			DeliveryModel:    deliveryModel,
		})
	}

	return BuildPlan{Apps: plans}
}

// resolveLanguageStrategy maps framework name to language strategy name.
// This is the single source of truth for framework→strategy mapping.
func resolveLanguageStrategy(framework string) string {
	switch framework {
	case "nextjs", "next", "vite", "remix", "nestjs", "express", "node",
		"react", "vue", "svelte", "astro", "angular", "fastify":
		return "node"
	case "gin", "go":
		return "go"
	case "fastapi", "django", "python":
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
