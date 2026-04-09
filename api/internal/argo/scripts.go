// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// scripts.go contains all shell script generation functions used by language strategies.
// Each function returns a shell script string that runs inside a build container.
package argo

import (
	"fmt"

	"github.com/loopzero/loopzero/api/internal/services"
)

// ---------------------------------------------------------------------------
// Node.js scripts
// ---------------------------------------------------------------------------

func scriptValidateNode(apps []services.AppSpec) string {
	paths := appPaths(apps)
	return `set -euo pipefail
cd /workspace
echo "Validating workspace..."
ERRORS=""
` + validatePathsScript(paths, "package.json") + `
# Check lockfile at root OR at any app path
LOCKFILES="package-lock.json yarn.lock pnpm-lock.yaml"
HAS_LOCK=false
for f in $LOCKFILES; do
  [ -f "$f" ] && HAS_LOCK=true
done
` + checkAppLockfiles(paths) + `
if [ "$HAS_LOCK" = "false" ]; then
  ERRORS="${ERRORS}No lockfile found. Run npm install and commit the lockfile. "
fi
if [ -n "$ERRORS" ]; then
  echo "ERROR: $ERRORS"
  mkdir -p /tmp/manifests
  echo "validate-workspace: $ERRORS" > /tmp/manifests/build-error.txt
  exit 1
fi
echo "Workspace validation passed"
`
}

func scriptInstallNode(apps []services.AppSpec) string {
	paths := appPaths(apps)
	script := "set -euo pipefail\ncd /workspace\n"
	for _, p := range paths {
		script += fmt.Sprintf(`echo "Installing deps in %s..."
cd "/workspace/%s"
if [ -f pnpm-lock.yaml ]; then
  corepack enable
  pnpm install --frozen-lockfile
elif [ -f yarn.lock ]; then
  corepack enable
  yarn install --frozen-lockfile
elif [ -f package-lock.json ]; then
  npm ci
else
  echo "WARNING: No lockfile in %s — running npm install"
  npm install
fi
echo "Dependencies installed in %s"
`, p, p, p, p)
	}
	return script
}

func scriptBuildNode(apps []services.AppSpec) string {
	paths := appPaths(apps)
	script := "set -euo pipefail\ncd /workspace\n"
	for _, p := range paths {
		script += fmt.Sprintf(`echo "Building %s..."
cd "/workspace/%s"
npm run build --if-present
echo "Build completed for %s"
`, p, p, p)
	}
	return script
}

func scriptScanNode() string {
	return `set -uo pipefail
cd /workspace
if [ -f package-lock.json ]; then
  npm audit --audit-level=critical 2>&1 || {
    echo "ERROR: npm audit found critical vulnerabilities"
    exit 1
  }
else
  echo "No package-lock.json found — skipping npm audit"
fi
`
}

// COMP-005: scriptDockerfileNode removed — was deprecated dead code (MEET-2026-03-29-017).
// Replaced by universal loopzeroPacksDockerfileStep() in languages.go.

// ---------------------------------------------------------------------------
// Go scripts
// ---------------------------------------------------------------------------

func scriptValidateGo(apps []services.AppSpec) string {
	paths := appPaths(apps)
	return `set -euo pipefail
cd /workspace
echo "Validating workspace..."
ERRORS=""
` + validatePathsScript(paths, "go.mod") + `
# Check go.sum exists
` + validatePathsScript(paths, "go.sum") + `
if [ -n "$ERRORS" ]; then
  echo "ERROR: $ERRORS"
  mkdir -p /tmp/manifests
  echo "validate-workspace: $ERRORS" > /tmp/manifests/build-error.txt
  exit 1
fi
echo "Workspace validation passed"
`
}

func scriptInstallGo(apps []services.AppSpec) string {
	paths := appPaths(apps)
	// CODE-005 fix: set GOCACHE to writable path (container runs as uid 1000, not root)
	script := "set -euo pipefail\nexport GOCACHE=/workspace/.cache/go-build\nmkdir -p $GOCACHE\ncd /workspace\n"
	for _, p := range paths {
		script += fmt.Sprintf(`echo "Downloading Go modules in %s..."
cd "/workspace/%s"
go mod download
echo "Modules downloaded in %s"
`, p, p, p)
	}
	return script
}

func scriptBuildGo(apps []services.AppSpec) string {
	paths := appPaths(apps)
	script := "set -euo pipefail\ncd /workspace\nmkdir -p /workspace/bin\n"
	for _, p := range paths {
		script += fmt.Sprintf(`echo "Building %s..."
cd "/workspace/%s"
CGO_ENABLED=0 go build -o /workspace/bin/%s ./...
echo "Build completed for %s"
`, p, p, sanitizeBinName(p), p)
	}
	return script
}

func scriptScanGo() string {
	return `set -uo pipefail
cd /workspace
if command -v govulncheck >/dev/null 2>&1; then
  govulncheck ./... 2>&1 || {
    echo "ERROR: govulncheck found vulnerabilities"
    exit 1
  }
else
  echo "govulncheck not available — skipping vulnerability scan"
fi
`
}

// COMP-005: scriptDockerfileGo removed — was deprecated dead code (MEET-2026-03-29-017).

// ---------------------------------------------------------------------------
// Python scripts
// ---------------------------------------------------------------------------

func scriptValidatePython(apps []services.AppSpec) string {
	paths := appPaths(apps)
	return `set -euo pipefail
cd /workspace
echo "Validating workspace..."
ERRORS=""
HAS_DEPS=false
` + validatePythonDeps(paths) + `
if [ "$HAS_DEPS" = "false" ]; then
  ERRORS="${ERRORS}No requirements.txt or pyproject.toml found. "
fi
if [ -n "$ERRORS" ]; then
  echo "ERROR: $ERRORS"
  mkdir -p /tmp/manifests
  echo "validate-workspace: $ERRORS" > /tmp/manifests/build-error.txt
  exit 1
fi
echo "Workspace validation passed"
`
}

func scriptInstallPython(apps []services.AppSpec) string {
	paths := appPaths(apps)
	// CODE-005 fix: set PIP_CACHE_DIR to writable path (container runs as uid 1000, not root)
	script := "set -euo pipefail\nexport PIP_CACHE_DIR=/workspace/.cache/pip\nmkdir -p $PIP_CACHE_DIR\ncd /workspace\n"
	for _, p := range paths {
		script += fmt.Sprintf(`echo "Installing deps in %s..."
cd "/workspace/%s"
if [ -f requirements.txt ]; then
  pip install --no-cache-dir -r requirements.txt
elif [ -f pyproject.toml ]; then
  pip install --no-cache-dir .
fi
echo "Dependencies installed in %s"
`, p, p, p)
	}
	return script
}

func scriptBuildPython(apps []services.AppSpec) string {
	paths := appPaths(apps)
	script := "set -euo pipefail\ncd /workspace\n"
	for _, p := range paths {
		script += fmt.Sprintf(`echo "Building %s..."
cd "/workspace/%s"
if [ -f pyproject.toml ]; then
  python -m build 2>/dev/null || echo "python -m build not available — skipping"
fi
echo "Build step completed for %s"
`, p, p, p)
	}
	return script
}

func scriptScanPython() string {
	return `set -uo pipefail
cd /workspace
if command -v pip-audit >/dev/null 2>&1; then
  pip-audit 2>&1 || {
    echo "ERROR: pip-audit found vulnerabilities"
    exit 1
  }
else
  pip install pip-audit 2>/dev/null && pip-audit 2>&1 || {
    echo "pip-audit not available or scan failed — skipping"
  }
fi
`
}

// COMP-005: scriptDockerfilePython removed — was deprecated dead code (MEET-2026-03-29-017).

// ---------------------------------------------------------------------------
// Custom scripts
// ---------------------------------------------------------------------------

func scriptValidateCustom() string {
	return `set -euo pipefail
cd /workspace
if [ ! -f Dockerfile ]; then
  echo "ERROR: Custom framework requires a Dockerfile in the project root"
  mkdir -p /tmp/manifests
  echo "validate-workspace: Custom framework requires a Dockerfile" > /tmp/manifests/build-error.txt
  exit 1
fi
echo "Dockerfile found — custom build validated"
`
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

// validatePathsScript generates shell code to check a file exists at each path.
func validatePathsScript(paths []string, filename string) string {
	script := ""
	for _, p := range paths {
		script += fmt.Sprintf(`if [ ! -f "%s/%s" ]; then
  ERRORS="${ERRORS}No %s found at %s/%s. "
fi
`, p, filename, filename, p, filename)
	}
	return script
}

// checkAppLockfiles generates shell to check lockfiles at app paths.
func checkAppLockfiles(paths []string) string {
	script := ""
	for _, p := range paths {
		script += fmt.Sprintf(`for f in $LOCKFILES; do
  [ -f "%s/$f" ] && HAS_LOCK=true
done
`, p)
	}
	return script
}

// scriptValidateFile generates a validate step checking a single file at each app path.
func scriptValidateFile(apps []services.AppSpec, filename string) string {
	paths := appPaths(apps)
	script := `set -euo pipefail
cd /workspace
echo "Validating workspace..."
ERRORS=""
`
	for _, p := range paths {
		script += fmt.Sprintf(`if [ ! -f "%s/%s" ]; then
  ERRORS="${ERRORS}No %s found at %s/%s. "
fi
`, p, filename, filename, p, filename)
	}
	return script + `if [ -n "$ERRORS" ]; then
  echo "ERROR: $ERRORS"
  mkdir -p /tmp/manifests
  echo "validate-workspace: $ERRORS" > /tmp/manifests/build-error.txt
  exit 1
fi
echo "Workspace validation passed"
`
}

// scriptValidateFileAny generates a validate step checking any of multiple files.
func scriptValidateFileAny(apps []services.AppSpec, filenames ...string) string {
	paths := appPaths(apps)
	script := `set -euo pipefail
cd /workspace
echo "Validating workspace..."
HAS_FILE=false
`
	for _, p := range paths {
		for _, f := range filenames {
			script += fmt.Sprintf(`[ -f "%s/%s" ] && HAS_FILE=true
`, p, f)
		}
	}
	files := ""
	for i, f := range filenames {
		if i > 0 {
			files += " or "
		}
		files += f
	}
	return script + fmt.Sprintf(`if [ "$HAS_FILE" = "false" ]; then
  echo "ERROR: No %s found"
  mkdir -p /tmp/manifests
  echo "validate-workspace: No %s found" > /tmp/manifests/build-error.txt
  exit 1
fi
echo "Workspace validation passed"
`, files, files)
}

// scriptInstallRuby generates shell to install Ruby dependencies.
func scriptInstallRuby(apps []services.AppSpec) string {
	paths := appPaths(apps)
	script := "set -euo pipefail\ncd /workspace\n"
	for _, p := range paths {
		script += fmt.Sprintf(`echo "Installing deps in %s..."
cd "/workspace/%s"
if [ -f Gemfile ]; then
  bundle install --without development test --path vendor/bundle
fi
echo "Dependencies installed in %s"
`, p, p, p)
	}
	return script
}

// validatePythonDeps generates shell to check Python dependency files.
func validatePythonDeps(paths []string) string {
	script := ""
	for _, p := range paths {
		script += fmt.Sprintf(`if [ -f "%s/requirements.txt" ] || [ -f "%s/pyproject.toml" ]; then
  HAS_DEPS=true
fi
`, p, p)
	}
	return script
}
