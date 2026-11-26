// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package argo provides real Argo Workflows integration for build pipeline execution.
//
// languages.go defines the LanguageStrategy interface and per-language implementations.
// Each strategy knows how to validate, install, build, scan, and generate Dockerfiles
// for a specific language ecosystem. The WorkflowBuilder composes these into Workflow CRs.
package argo

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/builds/shellgen"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
	"github.com/getloopscale/argo-workflows-go/model"
)

// LanguageStrategy defines language-specific build steps.
// Each method returns a forge.Container template that the WorkflowBuilder
// adds to the Workflow's template list.
type LanguageStrategy interface {
	// ValidateStep returns a template that validates the workspace (e.g., package.json exists).
	ValidateStep(apps []deploycontracts.AppSpec) *forge.Container
	// InstallStep returns a template that installs dependencies.
	InstallStep(apps []deploycontracts.AppSpec) *forge.Container
	// BuildStep returns a template that builds the application.
	BuildStep(apps []deploycontracts.AppSpec) *forge.Container
	// ScanStep returns a template that runs security scanning.
	ScanStep(apps []deploycontracts.AppSpec) *forge.Container
	// DockerfileStep returns a template that produces / validates the Dockerfile
	// before BuildKit consumes it. Every strategy now returns the canonical
	// generation container; this text used to also describe a `dockerfile-lint`
	// return for `UserDockerfileStrategy`, a type removed in T2.2 (B-157). See
	// `dockerfileStepTemplate`, which records the collapse of that variation.
	// totalApps is the count across ALL groups for output path naming
	// (matches dockerfilePathForApp / BuildKit consumer).
	//
	// build-mode-dispatch-and-dockerfile-security-plan T1.1 (2026-05-07):
	// restored as interface method; was removed in MEET-2026-03-29-017 when
	// loopruntime-packs went universal. The universal stance ignored the orthogonal
	// `build: dockerfile` axis. This restoration re-applies symmetry: each
	// strategy decides what runs in this phase.
	DockerfileStep(apps []deploycontracts.AppSpec, totalApps int) *forge.Container
	// CacheKey returns the cache key suffix for this language (e.g., "node-modules").
	CacheKey() string
	// CacheDirs returns directories to cache (e.g., ["node_modules"]).
	CacheDirs() []string
	// Name returns the strategy name for labeling (e.g., "node", "go", "python").
	Name() string
}

// SupportedFrameworks / IsKnownFramework removed (simplify-deploy-loopzerokit-
// runtime-wiring T2.1 / F-dom-2): the build pipeline no longer validates or
// dispatches on `framework`. The fixed LoopZeroKit stack is keyed on `runtime`
// (python|node) via ResolveStrategy; anything else fails closed (D1/D4).

// RuntimeDescriptor models a supported runtime as DATA rather than a hardcoded
// switch arm (M14 gap #1, blueprint Q1 — kserve's ServingRuntime treats a
// runtime as a declarative descriptor). Adding a runtime is a registry entry,
// not a code branch.
//
// D2: the registry is OPERATOR-ONLY config, never tenant-supplied — it is a
// package-level literal populated at init, so a tenant cannot inject a runtime.
// The fixed LoopZeroKit stack (python, node) stays fail-closed.
type RuntimeDescriptor struct {
	Name     string
	Strategy LanguageStrategy
}

// runtimeRegistry is the single source of truth for which runtimes resolve.
// Operator-only: to add a runtime, add a descriptor here — do NOT branch in
// ResolveStrategy. python + node only (D1/D4 fail-closed).
var runtimeRegistry = map[string]RuntimeDescriptor{
	"node":   {Name: "node", Strategy: &NodeStrategy{}},
	"python": {Name: "python", Strategy: &PythonStrategy{}},
}

// ResolveStrategy returns the LanguageStrategy for a `services.json` runtime by
// looking it up in runtimeRegistry (M14 gap #1 — data-driven, was a switch).
//
// EC-3: the lookup is EXACT-MATCH (case-sensitive, no trim), preserving the
// legacy switch semantics byte-for-byte. "Python", "python ", "" all miss and
// fail closed — normalizing would change behavior for existing callers.
//
// D4: there is NO empty-default-to-Node branch. `runtime` is required by the
// services.json schema, so an empty/unknown runtime at build time is a contract
// violation — it fails closed. Silent defaulting is precisely how a python app
// could be built as node.
func ResolveStrategy(runtime string) LanguageStrategy {
	if d, ok := runtimeRegistry[runtime]; ok {
		return d.Strategy
	}
	slog.Warn("unsupported runtime for the fixed LoopZeroKit stack — failing closed",
		"runtime", runtime, "supported", "python, node")
	return &unsupportedStrategy{runtime: runtime}
}

// unsupportedStrategy fails closed for any runtime outside the fixed LoopZeroKit
// stack (python, node). Every step emits a container that exits non-zero with
// a clear message, so a misrouted app fails fast at build time rather than
// silently building something wrong.
type unsupportedStrategy struct{ runtime string }

func (s *unsupportedStrategy) Name() string        { return "unsupported" }
func (s *unsupportedStrategy) CacheKey() string    { return "" }
func (s *unsupportedStrategy) CacheDirs() []string { return nil }

// failStep emits the step that fails loudly when the runtime is not part of the
// fixed stack.
//
// A MISSING field and an UNSUPPORTED value are different errors and used to
// share one message: a `loopzero.yaml` without `runtime` produced
// `runtime "" is not supported`, which accuses the user of mistyping something
// they never wrote and does not say what to write (B-154). Measured live on
// 2026-08-18, at the last link of the B-152 chain. Both messages are English,
// the language of every other build message.
func (s *unsupportedStrategy) failStep(name string) *forge.Container {
	var msg string
	if strings.TrimSpace(s.runtime) == "" {
		msg = "loopruntime: loopzero.yaml does not declare a `runtime` for this app — " +
			"add `runtime: python` or `runtime: node`. " +
			"LoopRuntimeCloud deploys the fixed LoopZeroKit stack and the runtime has no default: " +
			"guessing here would silently build the wrong image."
	} else {
		msg = fmt.Sprintf("loopruntime: runtime %q is not supported — LoopRuntimeCloud deploys the fixed LoopZeroKit stack (python, node)", s.runtime)
	}
	return buildContainer(name, imageAlpine, forge.ResourcesTiny(), failScript(msg))
}

// failScript is the body of a fail-closed step: it prints msg to stderr and exits
// 1. msg is single-quoted, so nothing in it (backticks, $(...), variables) is
// interpreted by the shell. Error text often carries user input, such as the
// runtime from loopzero.yaml, and markdown-style backticks.
func failScript(msg string) string {
	return "echo " + shellgen.ShellQuote(msg) + " >&2\nexit 1"
}

func (s *unsupportedStrategy) ValidateStep(_ []deploycontracts.AppSpec) *forge.Container {
	return s.failStep("validate-workspace")
}
func (s *unsupportedStrategy) InstallStep(_ []deploycontracts.AppSpec) *forge.Container {
	return s.failStep("install-deps")
}
func (s *unsupportedStrategy) BuildStep(_ []deploycontracts.AppSpec) *forge.Container {
	return s.failStep("build-apps")
}
func (s *unsupportedStrategy) ScanStep(_ []deploycontracts.AppSpec) *forge.Container {
	return s.failStep("security-scan")
}
func (s *unsupportedStrategy) DockerfileStep(_ []deploycontracts.AppSpec, _ int) *forge.Container {
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
func ResolveStrategyForApp(app deploycontracts.AppSpec) LanguageStrategy {
	return ResolveStrategy(app.Runtime)
}

// ---------------------------------------------------------------------------
// NodeStrategy — Node.js ecosystem (npm, yarn, pnpm)
// ---------------------------------------------------------------------------

// NodeStrategy handles Node.js builds (nextjs, express, vite, remix, nestjs, etc.).
type NodeStrategy struct{}

func (s *NodeStrategy) Name() string { return "node" }

func (s *NodeStrategy) CacheKey() string { return "node-modules" }

func (s *NodeStrategy) CacheDirs() []string { return []string{"node_modules"} }

func (s *NodeStrategy) ValidateStep(apps []deploycontracts.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imageNode, forge.ResourcesTiny(), scriptValidateNode(apps))
}

func (s *NodeStrategy) InstallStep(apps []deploycontracts.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imageNode, forge.ResourcesMedium(), scriptInstallNode(apps))
	c.RetryStrategy = forge.NewRetryOnFailure(3, "5s", "2", "60s")
	return c
}

func (s *NodeStrategy) BuildStep(apps []deploycontracts.AppSpec) *forge.Container {
	return buildContainer("build-apps", imageNode, forge.ResourcesMedium(), scriptBuildNode(apps))
}

func (s *NodeStrategy) ScanStep(_ []deploycontracts.AppSpec) *forge.Container {
	return buildContainer("security-scan", imageNode, forge.ResourcesSmall(), scriptScanNode())
}

func (s *NodeStrategy) DockerfileStep(apps []deploycontracts.AppSpec, totalApps int) *forge.Container {
	// T1.2: canonical platform Dockerfile (replaces loopruntime-packs).
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

func (s *PythonStrategy) ValidateStep(apps []deploycontracts.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", imagePython, forge.ResourcesTiny(), scriptValidatePython(apps))
}

func (s *PythonStrategy) InstallStep(apps []deploycontracts.AppSpec) *forge.Container {
	c := buildContainer("install-deps", imagePython, forge.ResourcesMedium(), scriptInstallPython(apps))
	c.RetryStrategy = forge.NewRetryOnFailure(3, "5s", "2", "60s")
	return c
}

func (s *PythonStrategy) BuildStep(apps []deploycontracts.AppSpec) *forge.Container {
	// REVIEW-LEVE-1: Python build step needs resourcesMedium (2Gi) like Node/Go.
	// Python projects with C extensions (numpy, pandas, tensorflow) compile from
	// source on Alpine and OOMKill at 256Mi.
	return buildContainer("build-apps", imagePython, forge.ResourcesMedium(), scriptBuildPython(apps))
}

func (s *PythonStrategy) ScanStep(apps []deploycontracts.AppSpec) *forge.Container {
	return buildContainer("security-scan", imagePython, forge.ResourcesSmall(), scriptScanPython(apps))
}

func (s *PythonStrategy) DockerfileStep(apps []deploycontracts.AppSpec, totalApps int) *forge.Container {
	// T1.2: canonical platform Dockerfile (replaces loopruntime-packs).
	return canonicalDockerfileStepOrFail("python", apps, totalApps)
}

// dockerfileStepTemplate is the name of the step EVERY strategy emits today.
//
// This was once a real variation point: the bring-your-own-Dockerfile path
// emitted `dockerfile-lint` while the language strategies emitted
// `generate-dockerfile`, and `buildMainDAG` had to ask the strategy which step
// it emitted to wire the dependencies. That path was REMOVED in T2.2 —
// `user_dockerfile_strategy_test.go` asserts the step is absent — and the
// variation collapsed without anything recording it.
//
// What was left was worse than a literal: a function that BUILT A THROWAWAY
// CONTAINER only to read its name (B-068). A discarded object tells no reader
// whether there is still variation; it forces each one to go and check.
//
// If the variation returns — a new build mode that emits another step — this
// is the place to restore the variation point, and
// `TestEveryStrategyEmitsTheSameDockerfileStepName` fails until someone does.
// Do NOT work around it by hard-coding a literal in the DAG: that is exactly
// the trap `CLAUDE.md` recorded while the two names coexisted.
const dockerfileStepTemplate = "generate-dockerfile"

// ---------------------------------------------------------------------------
// Helper: forge container builder
// ---------------------------------------------------------------------------

func buildContainer(name, image string, resources *forge.ResourceRequirements, script string) *forge.Container {
	// Plan J-17 Layer 2 (2026-06-06) — every step that flows through
	// buildContainer gets its script wrapped with user-facing log
	// markers (::loopruntime:: phase=X step=Y level=Z). Promtail extracts
	// the phase/step/level into Loki labels; the API's default LogQL
	// filter `{loopruntime_phase=~".+"}` then surfaces only labeled lines
	// to the dashboard "Logs" tab.
	wrapped := wrapWithLoopRuntimeMarkers(name, script)
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
