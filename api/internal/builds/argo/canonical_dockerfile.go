// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argo — canonical Dockerfile generation.
//
// Plan simplify-deploy-loopzerokit-only v2.0 (T1.1): replaces loopzero-packs (an
// 11-language heuristic Dockerfile generator) with two platform-controlled
// multi-stage Dockerfiles for the fixed LoopZeroKit stack (python + node).
//
// The generator runs at API time, BEFORE the source bundle is extracted in the
// build cluster, so it cannot inspect the project's lockfiles. Package-manager
// selection is therefore deterministic build-time shell logic baked into the
// Dockerfile itself (the SAME template for every node app, with a frozen-only
// install branch per manager). This keeps generation deterministic + auditable
// while honoring C2 (frozen installs; missing lockfile FAILs, no fallback).
package argo

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
)

// Canonical base images — C3: digest-pinned (resolved 2026-06-15 via
// `docker buildx imagetools inspect`). The format-gate test
// TestBaseImages_AreDigestPinned rejects tag-only pins; the real pull is
// proven at LIVE (Final Phase). Refresh deliberately + record in CHANGELOG.
const (
	imageNodeBase   = "node@sha256:0e910f435308c36ea60b4cfd7b80208044d77a074d16b768a81901ce938a62dc"   // node:22.12.0 (canonical 2026-06-16 — full Debian bookworm has bash 5.2 supporting `set -o pipefail`; slim variant uses dash which fails)
	imagePythonBase = "python@sha256:d764629ce0ddd8c71fd371e9901efb324a95789d2315a47db7e4d27e78f1b0e9" // python:3.12-slim
)

// canonicalDockerfile returns the platform-controlled multi-stage Dockerfile
// for an app on the fixed stack. runtime is "node" | "python" (resolved from
// the strategy). The returned text is byte-stable for a given input.
func canonicalDockerfile(runtime string, app deploycontracts.AppSpec) (string, error) {
	if app.Start == "" {
		return "", fmt.Errorf("canonical Dockerfile: app %q has no declared start command (services.json `start` is required — no heuristic CMD)", app.Name)
	}
	appDir := strings.TrimSpace(app.Path)
	if appDir == "" || appDir == "." {
		appDir = ""
	}

	switch runtime {
	case "node":
		return renderDockerfile(imageNodeBase, nodeInstall, nodeBuild(appDir), app, appDir), nil
	case "python":
		return renderDockerfile(imagePythonBase, pythonInstall, "", app, appDir), nil
	default:
		return "", fmt.Errorf("E-011: unsupported runtime %q — LoopZeroCloud deploys the fixed LoopZeroKit stack (python, node) only", runtime)
	}
}

// nodeInstall — frozen install with build-time package-manager detection (C2).
// Missing lockfile FAILs (no bare `npm install`).
const nodeInstall = `RUN set -eu; \
	if [ -f pnpm-lock.yaml ]; then corepack enable && pnpm install --frozen-lockfile; \
	elif [ -f yarn.lock ]; then corepack enable && yarn install --immutable; \
	elif [ -f package-lock.json ]; then npm ci; \
	else echo "loopzero: no lockfile (pnpm-lock.yaml / yarn.lock / package-lock.json) found in $(pwd) — dependencies are installed from the repository root, not from the app directory. Commit one of these files at the root, or move the app so its lockfile is there" >&2; exit 1; fi`

// pythonInstall — frozen install with build-time manager detection (C2).
const pythonInstall = `RUN set -eu; \
	if [ -f poetry.lock ]; then pip install --no-cache-dir poetry && poetry install --no-root --only main; \
	elif [ -f uv.lock ]; then pip install --no-cache-dir uv && uv sync --frozen; \
	elif [ -f requirements.txt ]; then pip install --no-cache-dir -r requirements.txt; \
	else echo "loopzero: no lockfile (requirements.txt / poetry.lock / uv.lock) found in $(pwd) — dependencies are installed from the repository root, not from the app directory. Commit one of these files at the root, or move the app so its lockfile is there" >&2; exit 1; fi`

// nodeBuild runs the project's build script if one is declared, in the target
// app directory (monorepo-aware, EC-1). Pure build-time shell — no API-side
// lockfile inspection.
func nodeBuild(appDir string) string {
	cd := ""
	if appDir != "" {
		cd = "cd " + appDir + " && "
	}
	return fmt.Sprintf(`RUN set -eu; \
	%sif [ -f package.json ] && grep -q '"build"' package.json; then \
	  if [ -f pnpm-lock.yaml ]; then corepack enable && pnpm run build; \
	  elif [ -f yarn.lock ]; then corepack enable && yarn build; \
	  else npm run build; fi; \
	fi`, cd)
}

// renderDockerfile assembles the multi-stage Dockerfile. EC-1: install happens
// at the workspace root (COPY . . copies the whole bundle); the build + runtime
// WORKDIR target appDir for monorepos. EC-7: EXPOSE only for server.
func renderDockerfile(base, install, build string, app deploycontracts.AppSpec, appDir string) string {
	runtimeWorkdir := "/app"
	if appDir != "" {
		runtimeWorkdir = "/app/" + appDir
	}

	var b strings.Builder
	b.WriteString("# syntax=docker/dockerfile:1\n")
	b.WriteString("FROM " + base + " AS build\n")
	b.WriteString("WORKDIR /app\n")
	b.WriteString("COPY . .\n")
	b.WriteString(install + "\n")
	if build != "" {
		b.WriteString(build + "\n")
	}
	b.WriteString("\n")
	b.WriteString("FROM " + base + " AS runtime\n")
	b.WriteString("WORKDIR " + runtimeWorkdir + "\n")
	b.WriteString("COPY --from=build /app /app\n")
	if app.Type != "worker" { // EC-7: workers receive no traffic — no EXPOSE.
		fmt.Fprintf(&b, "EXPOSE %d\n", app.Port)
	}
	// Shell-form CMD handles arbitrary declared start strings (e.g. "pnpm start").
	b.WriteString("CMD " + app.Start + "\n")
	return b.String()
}

// canonicalDockerfileStep returns the `generate-dockerfile` build step. It
// writes each app's canonical Dockerfile to its workspace path via a QUOTED
// heredoc (`<<'LOOPZERO_DF'`) so the Dockerfile body (incl. `$VAR`) is written
// verbatim — never expanded by the writer shell. Uses imageAlpine (minimal),
// NOT loopzero-packs. The step name is preserved for log-marker compatibility
// (loopzero_log.go:112). Per D4, the canonical file is written AFTER the source is
// in place, overwriting any user Dockerfile at the same path.
func canonicalDockerfileStep(runtime string, apps []deploycontracts.AppSpec, totalApps int) (*forge.Container, error) {
	var script strings.Builder
	script.WriteString("set -eu\n")
	// T2.3/D4: any user-supplied Dockerfile in the bundle is IGNORED — the
	// platform always builds from the canonical Dockerfile written below (which
	// overwrites the collision path), and BuildKit reads ONLY that path via
	// `--opt filename`. Warn so the user knows their file had no effect. This is
	// observability, not the control — the control is the truncating `cat >` +
	// the explicit BuildKit filename.
	script.WriteString("for __loopzero_df in $(find " + workspaceMountPath +
		" -maxdepth 3 \\( -iname 'Dockerfile' -o -iname 'Containerfile' -o -iname '*.dockerfile' \\) 2>/dev/null); do\n")
	script.WriteString("  echo \"::loopzero:: phase=build step=generate-dockerfile level=warn message=ignored user Dockerfile ${__loopzero_df} (LoopZeroCloud uses a managed build)\" >&2\n")
	script.WriteString("done\n")
	for _, app := range apps {
		df, err := canonicalDockerfile(runtime, app)
		if err != nil {
			return nil, err
		}
		out := dockerfilePathForApp(app.Name, totalApps)
		// %q shell-quotes the path (CODE-001 defense). The quoted heredoc
		// delimiter 'LOOPZERO_DF' disables expansion of the Dockerfile body.
		fmt.Fprintf(&script, "cat > %q <<'LOOPZERO_DF'\n%s\nLOOPZERO_DF\n", out, df)
	}
	return buildContainer("generate-dockerfile", imageAlpine, forge.ResourcesTiny(), script.String()), nil
}

// canonicalDockerfileStepOrFail adapts canonicalDockerfileStep to the
// LanguageStrategy.DockerfileStep signature (which returns no error). On a
// generation error (missing start / secret env / unsupported runtime — all of
// which should be caught upstream by validation) it returns a fail-closed
// container that exits non-zero at build time with a clear message, rather than
// panicking at plan time or silently falling back. The step name is preserved
// so the DAG wiring + log marker stay intact.
func canonicalDockerfileStepOrFail(runtime string, apps []deploycontracts.AppSpec, totalApps int) *forge.Container {
	c, err := canonicalDockerfileStep(runtime, apps, totalApps)
	if err != nil {
		msg := "loopzero: canonical Dockerfile generation failed: " + err.Error()
		return buildContainer("generate-dockerfile", imageAlpine, forge.ResourcesTiny(), failScript(msg))
	}
	return c
}
