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
	"os"

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
	// DockerfileStep returns a template that produces / validates the Dockerfile
	// before BuildKit consumes it. For language strategies (Node, Go, Python,
	// etc.) this returns the loopzero-packs auto-generation container. For
	// UserDockerfileStrategy it returns the dockerfile-lint container.
	// totalApps is the count across ALL groups for output path naming
	// (matches dockerfilePathForApp / BuildKit consumer).
	//
	// build-mode-dispatch-and-dockerfile-security-plan T1.1 (2026-05-07):
	// restored as interface method; was removed in MEET-2026-03-29-017 when
	// loopzero-packs went universal. The universal stance ignored the orthogonal
	// `build: dockerfile` axis. This restoration re-applies symmetry: each
	// strategy decides what runs in this phase.
	DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container
	// CacheKey returns the cache key suffix for this language (e.g., "node-modules").
	CacheKey() string
	// CacheDirs returns directories to cache (e.g., ["node_modules"]).
	CacheDirs() []string
	// Name returns the strategy name for labeling (e.g., "node", "go", "python").
	Name() string
}

// SupportedFrameworks returns the canonical set of framework string values
// the build pipeline understands. T5.4 of deep-review-2026-05-10-remediation-plan
// (find_arch_framework_dup MEDIUM) — single source of truth, replaces the
// duplicated `isUnknownFramework` switch that previously lived in
// routes/builds.go and could drift silently.
//
// The empty string is implicitly supported (defaults to Node.js — see
// ResolveStrategy fallback).
func SupportedFrameworks() map[string]bool {
	return map[string]bool{
		"nextjs": true, "next": true, "vite": true, "remix": true,
		"nestjs": true, "express": true, "node": true,
		"react": true, "vue": true, "svelte": true, "astro": true,
		"angular": true, "fastify": true,
		"gin": true, "go": true,
		"fastapi": true, "django": true, "python": true,
		"sinatra": true, "rails": true, "ruby": true,
		"spring": true, "spring-boot": true, "java": true,
		"laravel": true, "slim": true, "php": true,
		"axum": true, "actix": true, "rocket": true, "rust": true,
		"custom":          true,
		"user-dockerfile": true,
		"unknown":         true, // legacy alias accepted by callers
	}
}

// IsKnownFramework reports whether a framework string is in the
// SupportedFrameworks set. Empty string is treated as known (Node.js default).
func IsKnownFramework(framework string) bool {
	if framework == "" {
		return true
	}
	return SupportedFrameworks()[framework]
}

// ResolveStrategy returns the appropriate LanguageStrategy for a framework string.
// Framework values come from loopzero.yaml's app.framework field.
//
// NOTE: build-mode-dispatch-and-dockerfile-security-plan T1.4 (2026-05-07) —
// callers should prefer ResolveStrategyForApp(AppSpec) to honor the orthogonal
// `build: dockerfile` axis. This function remains for the case where only a
// framework name is available (e.g., language-grouping by string key).
func ResolveStrategy(framework string) LanguageStrategy {
	switch framework {
	case "nextjs", "vite", "remix", "nestjs", "express", "node":
		return &NodeStrategy{}
	case "gin", "go":
		return &GoStrategy{}
	case "fastapi", "django", "python":
		return &PythonStrategy{}
	case "sinatra", "rails", "ruby":
		return &RubyStrategy{}
	case "spring", "spring-boot", "java":
		return &JavaStrategy{}
	case "laravel", "slim", "php":
		return &PHPStrategy{}
	case "axum", "actix", "rocket", "rust":
		return &RustStrategy{}
	case "custom":
		return &CustomStrategy{}
	case "user-dockerfile":
		return &UserDockerfileStrategy{}
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

// ResolveStrategyForApp returns the LanguageStrategy for an AppSpec, honoring
// the orthogonal `build: dockerfile` axis: when the user opts into supplying
// their own Dockerfile, dispatch to UserDockerfileStrategy regardless of the
// declared framework.
//
// build-mode-dispatch-and-dockerfile-security-plan T1.4 (2026-05-07) — closes
// the architectural hole where MEET-2026-03-29-017 universalized loopzero-packs
// invocation, ignoring `build: dockerfile`. The strategy resolver is the
// idiomatic dispatch layer; pushing this decision into the orchestrator with
// an `if app.Build == "dockerfile"` would leak strategy semantics out of the
// pattern.
func ResolveStrategyForApp(app services.AppSpec) LanguageStrategy {
	if app.Build == "dockerfile" {
		return &UserDockerfileStrategy{}
	}
	return ResolveStrategy(app.Framework)
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
	return buildContainer("validate-workspace", imageNode, forge.ResourcesTiny(), scriptValidateNode(apps))
}

func (s *NodeStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imageNode, forge.ResourcesMedium(), scriptInstallNode(apps))
	c.RetryStrategy = forge.NewRetryOnFailure(3, "5s", "2", "60s")
	return c
}

func (s *NodeStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageNode, forge.ResourcesMedium(), scriptBuildNode(apps))
}

func (s *NodeStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageNode, forge.ResourcesSmall(), scriptScanNode())
}

func (s *NodeStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// GoStrategy — Go ecosystem
// ---------------------------------------------------------------------------

// GoStrategy handles Go builds (gin, standard Go apps, etc.).
type GoStrategy struct{}

func (s *GoStrategy) Name() string { return "go" }

func (s *GoStrategy) CacheKey() string { return "go-modules" }

// CacheDirs returns writable cache paths for Go module + build caches.
// CODE-005 fix: containers run as uid 1000 — /root/.cache is inaccessible;
// /workspace/.cache is writable by the build user.
func (s *GoStrategy) CacheDirs() []string {
	return []string{"/go/pkg/mod", "/workspace/.cache/go-build"}
}

func (s *GoStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageGolang, forge.ResourcesTiny(), scriptValidateGo(apps))
}

func (s *GoStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imageGolang, forge.ResourcesMedium(), scriptInstallGo(apps))
	c.RetryStrategy = forge.NewRetryOnFailure(3, "5s", "2", "60s")
	return c
}

func (s *GoStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageGolang, forge.ResourcesMedium(), scriptBuildGo(apps))
}

func (s *GoStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageGolang, forge.ResourcesSmall(), scriptScanGo())
}

func (s *GoStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// PythonStrategy — Python ecosystem
// ---------------------------------------------------------------------------

// PythonStrategy handles Python builds (fastapi, django, etc.).
type PythonStrategy struct{}

func (s *PythonStrategy) Name() string { return "python" }

func (s *PythonStrategy) CacheKey() string { return "pip-cache" }

// CacheDirs returns the writable pip cache path under /workspace.
// CODE-005 fix: containers run as uid 1000 — /root/.cache is inaccessible.
func (s *PythonStrategy) CacheDirs() []string { return []string{"/workspace/.cache/pip"} }

func (s *PythonStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imagePython, forge.ResourcesTiny(), scriptValidatePython(apps))
}

func (s *PythonStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imagePython, forge.ResourcesMedium(), scriptInstallPython(apps))
	c.RetryStrategy = forge.NewRetryOnFailure(3, "5s", "2", "60s")
	return c
}

func (s *PythonStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	// REVIEW-LEVE-1: Python build step needs resourcesMedium (2Gi) like Node/Go.
	// Python projects with C extensions (numpy, pandas, tensorflow) compile from
	// source on Alpine and OOMKill at 256Mi.
	return buildContainer("build-apps", imagePython, forge.ResourcesMedium(), scriptBuildPython(apps))
}

func (s *PythonStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imagePython, forge.ResourcesSmall(), scriptScanPython())
}

func (s *PythonStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// RubyStrategy — Ruby ecosystem (Bundler)
// ---------------------------------------------------------------------------

type RubyStrategy struct{}

func (s *RubyStrategy) Name() string                        { return "ruby" }
func (s *RubyStrategy) CacheKey() string                    { return "bundle-cache" }
func (s *RubyStrategy) CacheDirs() []string                 { return []string{"/workspace/vendor/bundle"} }
func (s *RubyStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageRuby, forge.ResourcesTiny(), scriptValidateFile(apps, "Gemfile"))
}
func (s *RubyStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", imageRuby, forge.ResourcesSmall(), scriptInstallRuby(apps))
}
func (s *RubyStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageRuby, forge.ResourcesTiny(), noopScript("Ruby — no compile step"))
}
func (s *RubyStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageRuby, forge.ResourcesTiny(), `set -euo pipefail
cd /workspace
gem install bundler-audit 2>/dev/null && bundle-audit check 2>/dev/null || echo "bundle-audit not available or scan failed — skipping"
`)
}
func (s *RubyStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// JavaStrategy — Java ecosystem (Maven/Gradle)
// ---------------------------------------------------------------------------

type JavaStrategy struct{}

func (s *JavaStrategy) Name() string                        { return "java" }
func (s *JavaStrategy) CacheKey() string                    { return "maven-cache" }
func (s *JavaStrategy) CacheDirs() []string                 { return []string{"/workspace/.m2", "/workspace/.gradle"} }
func (s *JavaStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageAlpine, forge.ResourcesTiny(), scriptValidateFileAny(apps, "pom.xml", "build.gradle", "build.gradle.kts"))
}
func (s *JavaStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", imageAlpine, forge.ResourcesTiny(), noopScript("Java — deps installed during build"))
}
func (s *JavaStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageAlpine, forge.ResourcesTiny(), noopScript("Java — build happens in Dockerfile (multi-stage)"))
}
func (s *JavaStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageAlpine, forge.ResourcesTiny(), noopScript("Java security scan — skipped (use Dockerfile-based scan)"))
}
func (s *JavaStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// PHPStrategy — PHP ecosystem (Composer)
// ---------------------------------------------------------------------------

type PHPStrategy struct{}

func (s *PHPStrategy) Name() string                        { return "php" }
func (s *PHPStrategy) CacheKey() string                    { return "composer-cache" }
func (s *PHPStrategy) CacheDirs() []string                 { return []string{"/workspace/vendor"} }
func (s *PHPStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageAlpine, forge.ResourcesTiny(), scriptValidateFile(apps, "composer.json"))
}
func (s *PHPStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", imageAlpine, forge.ResourcesTiny(), noopScript("PHP — deps installed in Dockerfile"))
}
func (s *PHPStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageAlpine, forge.ResourcesTiny(), noopScript("PHP — no compile step"))
}
func (s *PHPStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageAlpine, forge.ResourcesTiny(), noopScript("PHP security scan — skipped"))
}
func (s *PHPStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// RustStrategy — Rust ecosystem (Cargo)
// ---------------------------------------------------------------------------

type RustStrategy struct{}

func (s *RustStrategy) Name() string                        { return "rust" }
func (s *RustStrategy) CacheKey() string                    { return "cargo-cache" }
func (s *RustStrategy) CacheDirs() []string                 { return []string{"/workspace/target"} }
func (s *RustStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageAlpine, forge.ResourcesTiny(), scriptValidateFile(apps, "Cargo.toml"))
}
func (s *RustStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", imageAlpine, forge.ResourcesTiny(), noopScript("Rust — deps built during cargo build"))
}
func (s *RustStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageAlpine, forge.ResourcesTiny(), noopScript("Rust — build happens in Dockerfile (multi-stage)"))
}
func (s *RustStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageAlpine, forge.ResourcesTiny(), `set -euo pipefail
cd /workspace
cargo audit 2>/dev/null || echo "cargo-audit not available — skipping"
`)
}
func (s *RustStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// CustomStrategy — user provides their own Dockerfile
// ---------------------------------------------------------------------------

// CustomStrategy skips language-specific steps. Requires a user-provided Dockerfile.
type CustomStrategy struct{}

func (s *CustomStrategy) Name() string { return "custom" }

func (s *CustomStrategy) CacheKey() string { return "" }

func (s *CustomStrategy) CacheDirs() []string { return nil }

func (s *CustomStrategy) ValidateStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageAlpine, forge.ResourcesTiny(), scriptValidateCustom())
}

func (s *CustomStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", imageAlpine, forge.ResourcesTiny(), noopScript("No install step for custom framework — user manages dependencies in Dockerfile"))
}

func (s *CustomStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageAlpine, forge.ResourcesTiny(), noopScript("No build step for custom framework — build happens in Dockerfile"))
}

func (s *CustomStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageAlpine, forge.ResourcesTiny(), noopScript("No security scan for custom framework"))
}

// DockerfileStep — CustomStrategy means user manages everything in their
// Dockerfile (framework: custom). loopzero-packs v0.4.0 (lenient canonical
// contract) will copy the user-provided Dockerfile to the canonical output
// path verbatim. The result is a no-op from the user's perspective: their
// Dockerfile is what BuildKit consumes.
//
// T6.7 of deep-review-remediation-plan (closes find_arch_006 LOW). Previous
// comment claimed "Defense in depth" without explaining what was being
// defended; in reality loopzero-packs v0.4.0 is invariant under user Dockerfile
// (no rewriting). Distinct from UserDockerfileStrategy below: that variant
// runs loopzero-dockerfile-lint with hard rejection rules; CustomStrategy keeps
// the legacy behavior (no lint) for opt-out users on `framework: custom`.
func (s *CustomStrategy) DockerfileStep(apps []services.AppSpec, totalApps int) *forge.Container {
	return loopzeroPacksDockerfileStep(apps, totalApps)
}

// ---------------------------------------------------------------------------
// UserDockerfileStrategy — user opted into `build: dockerfile` in loopzero.yaml
// ---------------------------------------------------------------------------
//
// build-mode-dispatch-and-dockerfile-security-plan T1.3 (2026-05-07):
// distinct from CustomStrategy, which means `framework: custom` (user
// declared no language). UserDockerfileStrategy means "I'm bringing my own
// Dockerfile" — independent of language framework. LoopZero skips loopzero-packs
// invocation entirely (saves ~30s per app) and runs loopzero-dockerfile-lint
// against the user-supplied Dockerfile to enforce hard rejection rules
// (FROM allow-list, RUN flag prohibitions, bind mount escape, reserved
// env keys, seccomp/apparmor unconfined). Soft warnings audit-logged.

type UserDockerfileStrategy struct{}

func (s *UserDockerfileStrategy) Name() string                   { return "user-dockerfile" }
func (s *UserDockerfileStrategy) CacheKey() string               { return "" }
func (s *UserDockerfileStrategy) CacheDirs() []string            { return nil }

func (s *UserDockerfileStrategy) ValidateStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageAlpine, forge.ResourcesTiny(),
		noopScript("user-dockerfile build mode — workspace validation skipped (user-managed Dockerfile)"))
}

func (s *UserDockerfileStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", imageAlpine, forge.ResourcesTiny(),
		noopScript("user-dockerfile build mode — dependencies installed inside Dockerfile"))
}

func (s *UserDockerfileStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageAlpine, forge.ResourcesTiny(),
		noopScript("user-dockerfile build mode — build happens inside Dockerfile"))
}

func (s *UserDockerfileStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", imageAlpine, forge.ResourcesTiny(),
		noopScript("user-dockerfile build mode — security scan deferred to dockerfile-lint + post-build SBOM"))
}

func (s *UserDockerfileStrategy) DockerfileStep(apps []services.AppSpec, _ int) *forge.Container {
	return dockerfileLintStep(apps)
}

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
	return buildContainer("generate-dockerfile", loopzeroPacks(), forge.ResourcesSmall(), script)
}

// loopzeroPacks returns the loopzero-packs-runner image, allowing override via env var.
// Local dev sets LOOPZERO_PACKS_IMAGE to use the k3d registry image.
func loopzeroPacks() string {
	if v := os.Getenv("LOOPZERO_PACKS_IMAGE"); v != "" {
		return v
	}
	return imageLoopZeroPacks
}

// dockerfileStepTemplateName returns the template name a strategy's
// DockerfileStep emits — used by buildMainDAG to wire dependencies
// correctly. Avoids hardcoding "generate-dockerfile" in the orchestrator.
//
// build-mode-dispatch-and-dockerfile-security-plan T1.6 (2026-05-07).
func dockerfileStepTemplateName(s LanguageStrategy) string {
	// Probe with a single dummy app so we capture the actual container name.
	// All strategies must return a container with a stable Name across calls.
	c := s.DockerfileStep([]services.AppSpec{{Name: "probe", Path: "."}}, 1)
	if c == nil || c.Name == "" {
		// Fallback to legacy default.
		return "generate-dockerfile"
	}
	return c.Name
}

// ---------------------------------------------------------------------------
// Helper: forge container builder
// ---------------------------------------------------------------------------

func buildContainer(name, image string, resources *forge.ResourceRequirements, script string) *forge.Container {
	// Plan J-17 Layer 2 (2026-06-06) — every step that flows through
	// buildContainer gets its script wrapped with user-facing log
	// markers (::loopzero:: phase=X step=Y level=Z). Promtail extracts
	// the phase/step/level into Loki labels; the API's default LogQL
	// filter `{loopzero_phase=~".+"}` then surfaces only labeled lines
	// to the dashboard "Logs" tab.
	wrapped := wrapWithLoopZeroMarkers(name, script)
	return &forge.Container{
		Name:    name,
		Image:   image,
		Command: []string{"sh", "-c"},
		Args:    []string{wrapped},
		// REVIEW-LEVE-3: manifests mount removed — manifest generation moved to API (C-02).
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: volumeWorkspace, MountPath: workspaceMountPath}},
		},
		Resources: resources,
		SecurityContext: &model.SecurityContext{
			RunAsNonRoot: forge.Ptr(true),
			RunAsUser:    forge.Ptr(rootlessUID),
		},
	}
}

func noopScript(msg string) string {
	return fmt.Sprintf("echo %q", msg)
}
