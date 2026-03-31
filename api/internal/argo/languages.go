// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argo provides real Argo Workflows integration for build pipeline execution.
//
// languages.go defines the LanguageStrategy interface and per-language implementations.
// Each strategy knows how to validate, install, build, scan, and generate Dockerfiles
// for a specific language ecosystem. The WorkflowBuilder composes these into Workflow CRs.
package argo

import (
	"fmt"
	"log/slog"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"

	"github.com/loopzero/loopzero/api/internal/services"
)

// LanguageStrategy defines language-specific build steps.
// Each method returns a forge.Container template that the WorkflowBuilder
// adds to the Workflow's template list.
type LanguageStrategy interface {
	// ValidateStep returns a template that validates the workspace (e.g., package.json exists).
	ValidateStep(apps []services.AppSpec) *forge.Container
	// InstallStep returns a template that installs dependencies.
	InstallStep(apps []services.AppSpec) *forge.Container
	// BuildStep returns a template that builds the application.
	BuildStep(apps []services.AppSpec) *forge.Container
	// ScanStep returns a template that runs security scanning.
	ScanStep() *forge.Container
	// CacheKey returns the cache key suffix for this language (e.g., "node-modules").
	CacheKey() string
	// CacheDirs returns directories to cache (e.g., ["node_modules"]).
	CacheDirs() []string
	// Name returns the strategy name for labeling (e.g., "node", "go", "python").
	Name() string
}

// ResolveStrategy returns the appropriate LanguageStrategy for a framework string.
// Framework values come from loopzero.yaml's app.framework field.
func ResolveStrategy(framework string) LanguageStrategy {
	switch framework {
	case "nextjs", "vite", "remix", "nestjs", "express", "node":
		return &NodeStrategy{}
	case "gin", "go":
		return &GoStrategy{}
	case "fastapi", "django", "python":
		return &PythonStrategy{}
	case "custom":
		return &CustomStrategy{}
	default:
		// Empty framework defaults to Node.js for backwards compatibility.
		if framework == "" {
			return &NodeStrategy{}
		}
		slog.Warn("unrecognized framework, falling back to custom strategy",
			"framework", framework)
		return &CustomStrategy{}
	}
}

// ---------------------------------------------------------------------------
// NodeStrategy — Node.js ecosystem (npm, yarn, pnpm)
// ---------------------------------------------------------------------------

// NodeStrategy handles Node.js builds (nextjs, express, vite, remix, nestjs, etc.).
type NodeStrategy struct{}

func (s *NodeStrategy) Name() string { return "node" }

func (s *NodeStrategy) CacheKey() string { return "node-modules" }

func (s *NodeStrategy) CacheDirs() []string { return []string{"node_modules"} }

func (s *NodeStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageNode, resourcesTiny(), scriptValidateNode(apps))
}

func (s *NodeStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imageNode, resourcesMedium(), scriptInstallNode(apps))
	c.RetryStrategy = retryStrategy(3, "5s", "2", "60s")
	return c
}

func (s *NodeStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageNode, resourcesMedium(), scriptBuildNode(apps))
}

func (s *NodeStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageNode, resourcesSmall(), scriptScanNode())
}

// DockerfileStep removed (MEET-2026-03-29-017) — replaced by universal
// loopzeroPacksDockerfileStep() using loopzero-packs-runner container image.

// ---------------------------------------------------------------------------
// GoStrategy — Go ecosystem
// ---------------------------------------------------------------------------

// GoStrategy handles Go builds (gin, standard Go apps, etc.).
type GoStrategy struct{}

func (s *GoStrategy) Name() string { return "go" }

func (s *GoStrategy) CacheKey() string { return "go-modules" }

// CODE-005 fix: containers run as uid 1000 — /root/.cache is inaccessible.
// Use /workspace/.cache which is writable by the build user.
func (s *GoStrategy) CacheDirs() []string {
	return []string{"/go/pkg/mod", "/workspace/.cache/go-build"}
}

func (s *GoStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageGolang, resourcesTiny(), scriptValidateGo(apps))
}

func (s *GoStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imageGolang, resourcesMedium(), scriptInstallGo(apps))
	c.RetryStrategy = retryStrategy(3, "5s", "2", "60s")
	return c
}

func (s *GoStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageGolang, resourcesMedium(), scriptBuildGo(apps))
}

func (s *GoStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageGolang, resourcesSmall(), scriptScanGo())
}

// DockerfileStep removed — uses universal loopzeroPacksDockerfileStep().

// ---------------------------------------------------------------------------
// PythonStrategy — Python ecosystem
// ---------------------------------------------------------------------------

// PythonStrategy handles Python builds (fastapi, django, etc.).
type PythonStrategy struct{}

func (s *PythonStrategy) Name() string { return "python" }

func (s *PythonStrategy) CacheKey() string { return "pip-cache" }

// CODE-005 fix: containers run as uid 1000 — /root/.cache is inaccessible.
func (s *PythonStrategy) CacheDirs() []string { return []string{"/workspace/.cache/pip"} }

func (s *PythonStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imagePython, resourcesTiny(), scriptValidatePython(apps))
}

func (s *PythonStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imagePython, resourcesMedium(), scriptInstallPython(apps))
	c.RetryStrategy = retryStrategy(3, "5s", "2", "60s")
	return c
}

func (s *PythonStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	// REVIEW-LEVE-1: Python build step needs resourcesMedium (2Gi) like Node/Go.
	// Python projects with C extensions (numpy, pandas, tensorflow) compile from
	// source on Alpine and OOMKill at 256Mi.
	return buildContainer("build-apps", imagePython, resourcesMedium(), scriptBuildPython(apps))
}

func (s *PythonStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imagePython, resourcesSmall(), scriptScanPython())
}

// DockerfileStep removed — uses universal loopzeroPacksDockerfileStep().

// ---------------------------------------------------------------------------
// CustomStrategy — user provides their own Dockerfile
// ---------------------------------------------------------------------------

// CustomStrategy skips language-specific steps. Requires a user-provided Dockerfile.
type CustomStrategy struct{}

func (s *CustomStrategy) Name() string { return "custom" }

func (s *CustomStrategy) CacheKey() string { return "" }

func (s *CustomStrategy) CacheDirs() []string { return nil }

func (s *CustomStrategy) ValidateStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageAlpine, resourcesTiny(), scriptValidateCustom())
}

func (s *CustomStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", imageAlpine, resourcesTiny(), noopScript("No install step for custom framework — user manages dependencies in Dockerfile"))
}

func (s *CustomStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageAlpine, resourcesTiny(), noopScript("No build step for custom framework — build happens in Dockerfile"))
}

func (s *CustomStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageAlpine, resourcesTiny(), noopScript("No security scan for custom framework"))
}

// CustomStrategy still validates Dockerfile exists (no loopzero-packs needed).
// The DockerfileStep is only called from the universal step which skips
// generation when a user-provided Dockerfile exists.

// ---------------------------------------------------------------------------
// Universal Dockerfile generation via loopzero-packs (MEET-2026-03-29-017)
// ---------------------------------------------------------------------------

// loopzeroPacksDockerfileStep returns a container template that runs the
// loopzero-packs-runner image to analyze source code and generate an optimized
// Dockerfile. This replaces all per-language DockerfileStep implementations.
//
// For each app, it runs: loopzeropacks-generate --source /workspace --app-path <path>
//   --app-name <name> --output /workspace/Dockerfile.<name>
//
// If a user-provided Dockerfile exists, loopzeropacks-generate skips generation.
// loopzeroPacksDockerfileStep generates Dockerfiles for apps in a language group.
// totalApps is the count across ALL groups (for consistent naming with BuildKit consumer).
func loopzeroPacksDockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	script := "set -e\n"
	for _, app := range apps {
		appPath := app.Path
		if appPath == "" {
			appPath = "."
		}
		// Use shared naming function (MEET-2026-03-29-018).
		output := dockerfilePathForApp(app.Name, totalApps)
		// CODE-001 defense-in-depth: use %q for shell quoting of user-derived values.
		// Primary defense is ValidateAppName at the API boundary (builds.go).
		script += fmt.Sprintf(
			"loopzeropacks-generate --source %q --app-path %q --app-name %q --output %q\n",
			workspaceMountPath, appPath, app.Name, output,
		)
	}
	return buildContainer("generate-dockerfile", imageLoopZeroPacks, resourcesSmall(), script)
}

// ---------------------------------------------------------------------------
// Helper: forge container builder
// ---------------------------------------------------------------------------

func buildContainer(name, image string, resources *forge.ResourceRequirements, script string) *forge.Container {
	return &forge.Container{
		Name:    name,
		Image:   image,
		Command: []string{"sh", "-c"},
		Args:    []string{script},
		// REVIEW-LEVE-3: manifests mount removed — manifest generation moved to API (C-02).
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
		},
		Resources: resources,
		SecurityContext: &model.SecurityContext{
			RunAsNonRoot: ptr(true),
			RunAsUser:    ptr(rootlessUID),
		},
	}
}

func retryStrategy(limit int, duration, factor, maxDuration string) *forge.RetryStrategy {
	return &forge.RetryStrategy{
		Limit:       ptr(limit),
		RetryPolicy: forge.RetryPolicy("OnFailure"),
		Backoff: &forge.Backoff{
			Duration:    duration,
			Factor:      factor,
			MaxDuration: maxDuration,
		},
		// Skip retry on OOM kill (exit 137) — retrying will fail again with same memory limits.
		// Exit code 1 remains retryable (transient DNS/npm/registry failures).
		// MEET-2026-03-30-021 F3-C2: only exit 137 is irrecoverable.
		Expression: `asInt(lastRetry.exitCode) != 137`,
	}
}

func resourcesTiny() *forge.ResourceRequirements {
	return &forge.ResourceRequirements{
		Requests: forge.ResourceList{CPU: "50m", Memory: "32Mi"},
		Limits:   forge.ResourceList{CPU: "100m", Memory: "64Mi"},
	}
}

func resourcesSmall() *forge.ResourceRequirements {
	return &forge.ResourceRequirements{
		Requests: forge.ResourceList{CPU: "100m", Memory: "128Mi"},
		Limits:   forge.ResourceList{CPU: "500m", Memory: "256Mi"},
	}
}

func resourcesMedium() *forge.ResourceRequirements {
	return &forge.ResourceRequirements{
		Requests: forge.ResourceList{CPU: "500m", Memory: "512Mi"},
		Limits:   forge.ResourceList{CPU: "2000m", Memory: "2Gi"},
	}
}

func noopScript(msg string) string {
	return fmt.Sprintf("echo %q", msg)
}
