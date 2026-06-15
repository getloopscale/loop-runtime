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
	case "nextjs", "vite", "remix", "nestjs", "express", "node", "node-hono", "":
		// Empty framework defaults to Node (the dominant LoopZeroKit shape).
		return &NodeStrategy{}
	case "fastapi", "django", "python", "python-fastapi":
		return &PythonStrategy{}
	default:
		// T2.1 (plan simplify-deploy-loopzerokit-only v2.0): the fixed LoopZeroKit stack
		// is python + node ONLY. Every other runtime fails closed at build time
		// — no loopzero-packs, no silent fallback.
		slog.Warn("unsupported runtime for the fixed LoopZeroKit stack — failing closed",
			"runtime", framework, "supported", "python, node")
		return &unsupportedStrategy{framework: framework}
	}
}

// unsupportedStrategy fails closed for any runtime outside the fixed LoopZeroKit
// stack (python, node). Every step emits a container that exits non-zero with
// a clear message, so a misrouted app fails fast at build time rather than
// silently building something wrong.
type unsupportedStrategy struct{ framework string }

func (s *unsupportedStrategy) Name() string        { return "unsupported" }
func (s *unsupportedStrategy) CacheKey() string    { return "" }
func (s *unsupportedStrategy) CacheDirs() []string { return nil }

func (s *unsupportedStrategy) failStep(name string) *forge.Container {
	msg := fmt.Sprintf("loopzero: runtime %q is not supported — LoopZeroCloud deploys the fixed LoopZeroKit stack (python, node) only", s.framework)
	return buildContainer(name, imageAlpine, forge.ResourcesTiny(), fmt.Sprintf("echo %q >&2\nexit 1", msg))
}

func (s *unsupportedStrategy) ValidateStep(_ []services.AppSpec) *forge.Container {
	return s.failStep("validate-workspace")
}
func (s *unsupportedStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return s.failStep("install-deps")
}
func (s *unsupportedStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return s.failStep("build-apps")
}
func (s *unsupportedStrategy) ScanStep() *forge.Container { return s.failStep("security-scan") }
func (s *unsupportedStrategy) DockerfileStep(_ []services.AppSpec, _ int) *forge.Container {
	return s.failStep("generate-dockerfile")
}

// ResolveStrategyForApp returns the LanguageStrategy for an AppSpec.
//
// T2.2 (plan simplify-deploy-loopzerokit-only v2.0): the BYO-Dockerfile path
// (`build: dockerfile` → UserDockerfileStrategy) is REMOVED — letting a user
// supply their own Dockerfile is a build-escape/RCE surface (the build sandbox
// runs unconfined). The platform now always uses its canonical Dockerfile; any
// user Dockerfile in the bundle is ignored fail-closed (D4, enforced in the
// build-context assembly — T2.3). Dispatch is purely runtime-based.
func ResolveStrategyForApp(app services.AppSpec) LanguageStrategy {
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
	// T1.2: canonical platform Dockerfile (replaces loopzero-packs).
	return canonicalDockerfileStepOrFail("node", apps, totalApps)
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
	// T1.2: canonical platform Dockerfile (replaces loopzero-packs).
	return canonicalDockerfileStepOrFail("python", apps, totalApps)
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
