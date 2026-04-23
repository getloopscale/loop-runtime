// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// scripts.go contains all shell script generation functions used by language strategies.
// Each function returns a shell script string that runs inside a build container.
//
// All user-supplied paths (from loopzero.yaml) are escaped via shellgen.ShellQuote
// to prevent shell injection. Complex shell constructs use Raw() where they
// don't involve user input.
package argo

import (
	"fmt"
	"strings"

	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/shellgen"
)

// ---------------------------------------------------------------------------
// Node.js scripts
// ---------------------------------------------------------------------------

func scriptValidateNode(apps []services.AppSpec) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace").
		Echo("Validating workspace...").
		Raw(`ERRORS=""`)

	// Check package.json exists at each app path
	appendValidateFile(b, paths, "package.json")

	// Check lockfile at root OR at any app path
	b.Raw(`LOCKFILES="package-lock.json yarn.lock pnpm-lock.yaml"`)
	b.Raw(`HAS_LOCK=false`)
	b.Raw(`for f in $LOCKFILES; do`)
	b.Raw(`  [ -f "$f" ] && HAS_LOCK=true`)
	b.Raw(`done`)

	for _, p := range paths {
		b.Raw(`for f in $LOCKFILES; do`)
		b.Raw(fmt.Sprintf(`  [ -f %s"/$f" ] && HAS_LOCK=true`, shellgen.ShellQuote(p)))
		b.Raw(`done`)
	}

	// Lockfile check: warn if missing, don't block. The Dockerfile or install step
	// handles the actual failure with a clear message. Model: Vercel/Railway behavior —
	// lockfile absence is the user's responsibility, not a platform gate.
	// If npm ci is in their Dockerfile, Docker itself fails clearly.
	// If no Dockerfile, scriptInstallNode falls back to npm install with a warning.
	b.Raw(`if [ "$HAS_LOCK" = "false" ]; then`)
	b.Raw(`  echo "WARNING: No lockfile found. Builds may be non-deterministic. Consider running npm install and committing the lockfile."`)
	b.Raw(`fi`)

	appendErrorExit(b)
	b.Echo("Workspace validation passed")
	return b.String() + "\n"
}

func scriptInstallNode(apps []services.AppSpec) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace")
	for _, p := range paths {
		dir := "/workspace/" + p
		b.Echo("Installing deps in " + p + "...").
			Cd(dir).
			Raw(`if [ -f pnpm-lock.yaml ]; then`).
			Raw(`  corepack enable`).
			Raw(`  pnpm install --frozen-lockfile`).
			Raw(`elif [ -f yarn.lock ]; then`).
			Raw(`  corepack enable`).
			Raw(`  yarn install --frozen-lockfile`).
			Raw(`elif [ -f package-lock.json ]; then`).
			Raw(`  npm ci`).
			Raw(`else`).
			Raw(fmt.Sprintf(`  echo "WARNING: No lockfile in %s — running npm install"`, shellgen.ShellQuote(p))).
			Raw(`  npm install`).
			Raw(`fi`).
			Echo("Dependencies installed in " + p)
	}
	return b.String() + "\n"
}

func scriptBuildNode(apps []services.AppSpec) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace")
	for _, p := range paths {
		dir := "/workspace/" + p
		b.Echo("Building " + p + "...").
			Cd(dir).
			Run("npm", "run", "build", "--if-present").
			Echo("Build completed for " + p)
	}
	return b.String() + "\n"
}

func scriptScanNode() string {
	b := shellgen.New().Raw("set -uo pipefail").Cd("/workspace")
	b.Raw(`if [ -f package-lock.json ]; then`).
		Raw(`  npm audit --audit-level=critical 2>&1 || {`).
		Raw(`    echo "ERROR: npm audit found critical vulnerabilities"`).
		Raw(`    exit 1`).
		Raw(`  }`).
		Raw(`else`).
		Raw(`  echo "No package-lock.json found — skipping npm audit"`).
		Raw(`fi`)
	return b.String() + "\n"
}

// COMP-005: scriptDockerfileNode removed — was deprecated dead code (MEET-2026-03-29-017).
// Replaced by universal loopzeroPacksDockerfileStep() in languages.go.

// ---------------------------------------------------------------------------
// Go scripts
// ---------------------------------------------------------------------------

func scriptValidateGo(apps []services.AppSpec) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace").
		Echo("Validating workspace...").
		Raw(`ERRORS=""`)

	appendValidateFile(b, paths, "go.mod")
	// go.sum: warn if missing, don't block. go mod download generates it.
	// The Dockerfile handles this via `COPY go.sum* ./` (wildcard).
	appendWarnFile(b, paths, "go.sum")

	appendErrorExit(b)
	b.Echo("Workspace validation passed")
	return b.String() + "\n"
}

func scriptInstallGo(apps []services.AppSpec) string {
	paths := appPaths(apps)
	// CODE-005 fix: set GOCACHE to writable path (container runs as uid 1000, not root)
	b := shellgen.New().SetE().
		Env("GOCACHE", "/workspace/.cache/go-build").
		MkdirP("/workspace/.cache/go-build").
		Cd("/workspace")

	for _, p := range paths {
		dir := "/workspace/" + p
		b.Echo("Downloading Go modules in " + p + "...").
			Cd(dir).
			Run("go", "mod", "download").
			Echo("Modules downloaded in " + p)
	}
	return b.String() + "\n"
}

func scriptBuildGo(apps []services.AppSpec) string {
	paths := appPaths(apps)
	// CODE-005 (extended): set GOCACHE to writable path — same fix as scriptInstallGo.
	// Without this, `go build` writes to default $HOME/.cache/go-build which resolves
	// to /.cache/go-build when running as uid 1000 in the Argo container (no perms).
	b := shellgen.New().SetE().
		Env("GOCACHE", "/workspace/.cache/go-build").
		MkdirP("/workspace/.cache/go-build").
		Cd("/workspace").MkdirP("/workspace/bin")
	for _, p := range paths {
		dir := "/workspace/" + p
		binName := sanitizeBinName(p)
		b.Echo("Building " + p + "...").
			Cd(dir).
			Raw(fmt.Sprintf("CGO_ENABLED=0 go build -o %s ./...", shellgen.ShellQuote("/workspace/bin/"+binName))).
			Echo("Build completed for " + p)
	}
	return b.String() + "\n"
}

func scriptScanGo() string {
	b := shellgen.New().Raw("set -uo pipefail").Cd("/workspace")
	b.Raw(`if command -v govulncheck >/dev/null 2>&1; then`).
		Raw(`  govulncheck ./... 2>&1 || {`).
		Raw(`    echo "ERROR: govulncheck found vulnerabilities"`).
		Raw(`    exit 1`).
		Raw(`  }`).
		Raw(`else`).
		Raw(`  echo "govulncheck not available — skipping vulnerability scan"`).
		Raw(`fi`)
	return b.String() + "\n"
}

// COMP-005: scriptDockerfileGo removed — was deprecated dead code (MEET-2026-03-29-017).

// ---------------------------------------------------------------------------
// Python scripts
// ---------------------------------------------------------------------------

func scriptValidatePython(apps []services.AppSpec) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace").
		Echo("Validating workspace...").
		Raw(`ERRORS=""`).
		Raw(`HAS_DEPS=false`)

	for _, p := range paths {
		reqPath := shellgen.ShellQuote(p + "/requirements.txt")
		pyPath := shellgen.ShellQuote(p + "/pyproject.toml")
		b.Raw(fmt.Sprintf(`if [ -f %s ] || [ -f %s ]; then`, reqPath, pyPath)).
			Raw(`  HAS_DEPS=true`).
			Raw(`fi`)
	}

	b.Raw(`if [ "$HAS_DEPS" = "false" ]; then`).
		Raw(`  ERRORS="${ERRORS}No requirements.txt or pyproject.toml found. "`).
		Raw(`fi`)

	appendErrorExit(b)
	b.Echo("Workspace validation passed")
	return b.String() + "\n"
}

func scriptInstallPython(apps []services.AppSpec) string {
	paths := appPaths(apps)
	// CODE-005 fix: set PIP_CACHE_DIR to writable path (container runs as uid 1000, not root)
	b := shellgen.New().SetE().
		Env("PIP_CACHE_DIR", "/workspace/.cache/pip").
		MkdirP("/workspace/.cache/pip").
		Cd("/workspace")

	for _, p := range paths {
		dir := "/workspace/" + p
		b.Echo("Installing deps in " + p + "...").
			Cd(dir).
			Raw(`if [ -f requirements.txt ]; then`).
			Raw(`  pip install --no-cache-dir -r requirements.txt`).
			Raw(`elif [ -f pyproject.toml ]; then`).
			Raw(`  pip install --no-cache-dir .`).
			Raw(`fi`).
			Echo("Dependencies installed in " + p)
	}
	return b.String() + "\n"
}

func scriptBuildPython(apps []services.AppSpec) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace")
	for _, p := range paths {
		dir := "/workspace/" + p
		b.Echo("Building " + p + "...").
			Cd(dir).
			Raw(`if [ -f pyproject.toml ]; then`).
			Raw(`  python -m build 2>/dev/null || echo "python -m build not available — skipping"`).
			Raw(`fi`).
			Echo("Build step completed for " + p)
	}
	return b.String() + "\n"
}

func scriptScanPython() string {
	b := shellgen.New().Raw("set -uo pipefail").Cd("/workspace")
	b.Raw(`if command -v pip-audit >/dev/null 2>&1; then`).
		Raw(`  pip-audit 2>&1 || {`).
		Raw(`    echo "ERROR: pip-audit found vulnerabilities"`).
		Raw(`    exit 1`).
		Raw(`  }`).
		Raw(`else`).
		Raw(`  pip install pip-audit 2>/dev/null && pip-audit 2>&1 || {`).
		Raw(`    echo "pip-audit not available or scan failed — skipping"`).
		Raw(`  }`).
		Raw(`fi`)
	return b.String() + "\n"
}

// COMP-005: scriptDockerfilePython removed — was deprecated dead code (MEET-2026-03-29-017).

// ---------------------------------------------------------------------------
// Custom scripts
// ---------------------------------------------------------------------------

func scriptValidateCustom() string {
	b := shellgen.New().SetE().Cd("/workspace")
	b.IfFileNotExists("/workspace/Dockerfile").
		Then(func(inner *shellgen.ScriptBuilder) {
			inner.Echo("ERROR: Custom framework requires a Dockerfile in the project root").
				MkdirP("/tmp/manifests").
				Raw(`echo "validate-workspace: Custom framework requires a Dockerfile" > /tmp/manifests/build-error.txt`).
				Raw("exit 1")
		}).
		EndIf()
	b.Echo("Dockerfile found — custom build validated")
	return b.String() + "\n"
}

// ---------------------------------------------------------------------------
// Shared script helpers
// ---------------------------------------------------------------------------

// appPaths extracts the workspace-relative paths from app specs.
// Missing paths fall back to "." for backwards compatibility.
func appPaths(apps []services.AppSpec) []string {
	if len(apps) == 0 {
		return []string{"."}
	}
	paths := make([]string, len(apps))
	for i, a := range apps {
		if a.Path != "" {
			paths[i] = a.Path
		} else {
			paths[i] = "."
		}
	}
	return paths
}

// defaultPort extracts the first non-zero port from apps, or returns fallback.
func defaultPort(apps []services.AppSpec, fallback int) int {
	for _, a := range apps {
		if a.Port > 0 {
			return a.Port
		}
	}
	return fallback
}

// sanitizeBinName converts a path to a safe binary name.
func sanitizeBinName(path string) string {
	if path == "" || path == "." {
		return "app"
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

// appendValidateFile appends shell code to check a file exists at each path.
func appendValidateFile(b *shellgen.ScriptBuilder, paths []string, filename string) {
	for _, p := range paths {
		filePath := shellgen.ShellQuote(p + "/" + filename)
		b.Raw(fmt.Sprintf("if [ ! -f %s ]; then", filePath)).
			Raw(fmt.Sprintf(`  ERRORS="${ERRORS}No %s found at %s/%s. "`, filename, p, filename)).
			Raw("fi")
	}
}

// appendWarnFile appends shell code to WARN (not error) if a file is missing.
// Used for optional lockfiles where absence is the user's choice, not a platform gate.
func appendWarnFile(b *shellgen.ScriptBuilder, paths []string, filename string) {
	for _, p := range paths {
		filePath := shellgen.ShellQuote(p + "/" + filename)
		b.Raw(fmt.Sprintf("if [ ! -f %s ]; then", filePath)).
			Raw(fmt.Sprintf(`  echo "WARNING: No %s found at %s/%s — build may be non-deterministic"`, filename, p, filename)).
			Raw("fi")
	}
}

// appendErrorExit appends the standard error-exit block.
func appendErrorExit(b *shellgen.ScriptBuilder) {
	b.Raw(`if [ -n "$ERRORS" ]; then`).
		Raw(`  echo "ERROR: $ERRORS"`).
		MkdirP("/tmp/manifests").
		Raw(`  echo "validate-workspace: $ERRORS" > /tmp/manifests/build-error.txt`).
		Raw(`  exit 1`).
		Raw(`fi`)
}

// scriptValidateFile generates a validate step checking a single file at each app path.
func scriptValidateFile(apps []services.AppSpec, filename string) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace").
		Echo("Validating workspace...").
		Raw(`ERRORS=""`)

	appendValidateFile(b, paths, filename)
	appendErrorExit(b)
	b.Echo("Workspace validation passed")
	return b.String() + "\n"
}

// scriptValidateFileAny generates a validate step checking any of multiple files.
func scriptValidateFileAny(apps []services.AppSpec, filenames ...string) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace").
		Echo("Validating workspace...").
		Raw(`HAS_FILE=false`)

	for _, p := range paths {
		for _, f := range filenames {
			filePath := shellgen.ShellQuote(p + "/" + f)
			b.Raw(fmt.Sprintf("[ -f %s ] && HAS_FILE=true", filePath))
		}
	}

	files := strings.Join(filenames, " or ")
	b.Raw(`if [ "$HAS_FILE" = "false" ]; then`).
		Raw(fmt.Sprintf(`  echo "ERROR: No %s found"`, files)).
		MkdirP("/tmp/manifests").
		Raw(fmt.Sprintf(`  echo "validate-workspace: No %s found" > /tmp/manifests/build-error.txt`, files)).
		Raw(`  exit 1`).
		Raw(`fi`)

	b.Echo("Workspace validation passed")
	return b.String() + "\n"
}

// scriptInstallRuby generates shell to install Ruby dependencies.
func scriptInstallRuby(apps []services.AppSpec) string {
	paths := appPaths(apps)
	b := shellgen.New().SetE().Cd("/workspace")
	for _, p := range paths {
		dir := "/workspace/" + p
		b.Echo("Installing deps in " + p + "...").
			Cd(dir).
			Raw(`if [ -f Gemfile ]; then`).
			Raw(`  bundle install --without development test --path vendor/bundle`).
			Raw(`fi`).
			Echo("Dependencies installed in " + p)
	}
	return b.String() + "\n"
}
