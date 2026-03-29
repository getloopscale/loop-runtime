// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// scripts.go contains all shell script generation functions used by language strategies.
// Each function returns a shell script string that runs inside a build container.
package argo

import (
	"fmt"
	"strings"

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

// Deprecated: replaced by loopzeroPacksDockerfileStep (MEET-2026-03-29-017).
// Kept temporarily for rollback safety. Remove after loopzero-packs is validated in production.
func scriptDockerfileNode(apps []services.AppSpec) string { //nolint:unused
	if len(apps) == 0 {
		return `echo "No apps to generate Dockerfiles for"`
	}

	var script strings.Builder
	script.WriteString("set -euo pipefail\ncd /workspace\n")

	for _, app := range apps {
		appPath := app.Path
		if appPath == "" || appPath == "." {
			appPath = "."
		}

		dockerfileName := "Dockerfile"
		if len(apps) > 1 {
			dockerfileName = fmt.Sprintf("Dockerfile.%s", app.Name)
		}

		fmt.Fprintf(&script,
			"if [ -f \"%s/Dockerfile\" ]; then\n  echo \"User Dockerfile found in %s\"\n  cp \"%s/Dockerfile\" \"%s\"\nelse\n",
			appPath, appPath, appPath, dockerfileName)

		var df strings.Builder
		df.WriteString("FROM node:20-alpine\n")
		df.WriteString("WORKDIR /app\n")

		if appPath == "." {
			df.WriteString("COPY package.json package-lock.json ./\n")
			df.WriteString("RUN npm ci --omit=dev\n")
			df.WriteString("COPY . .\n")
		} else {
			df.WriteString("COPY package.json package-lock.json ./\n")
			fmt.Fprintf(&df, "COPY %s ./%s\n", appPath, appPath)
			df.WriteString("RUN npm ci --omit=dev 2>/dev/null || true\n")
		}

		fmt.Fprintf(&df, "WORKDIR /app/%s\n", appPath)

		if app.Type != "worker" {
			port := app.Port
			if port <= 0 {
				port = 3000
			}
			fmt.Fprintf(&df, "EXPOSE %d\n", port)
		}
		df.WriteString("CMD [\"node\", \"index.js\"]\n")

		content := strings.ReplaceAll(df.String(), "%", "%%")
		content = strings.ReplaceAll(content, "\\", "\\\\")
		fmt.Fprintf(&script, "  echo 'Generating %s for %s'\n", dockerfileName, app.Name)
		fmt.Fprintf(&script, "  printf '%s' > \"%s\"\n", content, dockerfileName)
		script.WriteString("fi\n")
	}

	script.WriteString("echo 'Dockerfiles generated:'\nls -la /workspace/Dockerfile*\nfor f in /workspace/Dockerfile*; do echo \"--- $f ---\"; cat \"$f\"; done\n")
	return script.String()
}

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
	script := "set -euo pipefail\ncd /workspace\n"
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

// Deprecated: replaced by loopzeroPacksDockerfileStep (MEET-2026-03-29-017).
func scriptDockerfileGo(apps []services.AppSpec) string { //nolint:unused
	port := defaultPort(apps, 8080)
	buildTarget := "./cmd/..."
	if len(apps) > 0 && apps[0].Path != "" && apps[0].Path != "." {
		buildTarget = fmt.Sprintf("./%s/...", apps[0].Path)
	}
	return fmt.Sprintf(`set -euo pipefail
cd /workspace
if [ -f Dockerfile ]; then
  echo "User-provided Dockerfile found — skipping generation"
  cat Dockerfile
  exit 0
fi
echo "No Dockerfile found — generating for Go"
cat > Dockerfile << 'DEOF'
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /app %s

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /app /app
EXPOSE %d
ENTRYPOINT ["/app"]
DEOF
echo "Generated Dockerfile:"
cat Dockerfile
`, buildTarget, port)
}

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
	script := "set -euo pipefail\ncd /workspace\n"
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

// Deprecated: replaced by loopzeroPacksDockerfileStep (MEET-2026-03-29-017).
func scriptDockerfilePython(apps []services.AppSpec) string { //nolint:unused
	port := defaultPort(apps, 8000)
	return fmt.Sprintf(`set -euo pipefail
cd /workspace
if [ -f Dockerfile ]; then
  echo "User-provided Dockerfile found — skipping generation"
  cat Dockerfile
  exit 0
fi
echo "No Dockerfile found — generating for Python"
cat > Dockerfile << 'DEOF'
FROM python:3.12-slim
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || pip install --no-cache-dir .
EXPOSE %d
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "%d"]
DEOF
echo "Generated Dockerfile:"
cat Dockerfile
`, port, port)
}

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
