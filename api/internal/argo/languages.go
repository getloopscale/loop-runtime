// Package argo provides real Argo Workflows integration for build pipeline execution.
//
// languages.go defines the LanguageStrategy interface and per-language implementations.
// Each strategy knows how to validate, install, build, scan, and generate Dockerfiles
// for a specific language ecosystem. The WorkflowBuilder composes these into Workflow CRs.
package argo

import (
	"fmt"

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
	// DockerfileStep returns a template that generates a Dockerfile if missing.
	DockerfileStep(apps []services.AppSpec) *forge.Container
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
		// Empty or unrecognized framework defaults to Node.js for backwards compatibility.
		// This matches the current monolithic pipeline behavior.
		if framework == "" {
			return &NodeStrategy{}
		}
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
	return buildContainer("validate-workspace", "node:20.11.1-alpine", resourcesTiny(), scriptValidateNode(apps))
}

func (s *NodeStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", "node:20.11.1-alpine", resourcesMedium(), scriptInstallNode(apps))
	c.RetryStrategy = retryStrategy(3, "5s", "2", "60s")
	return c
}

func (s *NodeStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", "node:20.11.1-alpine", resourcesMedium(), scriptBuildNode(apps))
}

func (s *NodeStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", "node:20.11.1-alpine", resourcesSmall(), scriptScanNode())
}

func (s *NodeStrategy) DockerfileStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("generate-dockerfile", "node:20.11.1-alpine", resourcesSmall(), scriptDockerfileNode(apps))
}

// ---------------------------------------------------------------------------
// GoStrategy — Go ecosystem
// ---------------------------------------------------------------------------

// GoStrategy handles Go builds (gin, standard Go apps, etc.).
type GoStrategy struct{}

func (s *GoStrategy) Name() string { return "go" }

func (s *GoStrategy) CacheKey() string { return "go-modules" }

func (s *GoStrategy) CacheDirs() []string { return []string{"/go/pkg/mod", "/root/.cache/go-build"} }

func (s *GoStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", "golang:1.22-alpine", resourcesTiny(), scriptValidateGo(apps))
}

func (s *GoStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", "golang:1.22-alpine", resourcesMedium(), scriptInstallGo(apps))
	c.RetryStrategy = retryStrategy(3, "5s", "2", "60s")
	return c
}

func (s *GoStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", "golang:1.22-alpine", resourcesMedium(), scriptBuildGo(apps))
}

func (s *GoStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", "golang:1.22-alpine", resourcesSmall(), scriptScanGo())
}

func (s *GoStrategy) DockerfileStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("generate-dockerfile", "golang:1.22-alpine", resourcesSmall(), scriptDockerfileGo(apps))
}

// ---------------------------------------------------------------------------
// PythonStrategy — Python ecosystem
// ---------------------------------------------------------------------------

// PythonStrategy handles Python builds (fastapi, django, etc.).
type PythonStrategy struct{}

func (s *PythonStrategy) Name() string { return "python" }

func (s *PythonStrategy) CacheKey() string { return "pip-cache" }

func (s *PythonStrategy) CacheDirs() []string { return []string{"/root/.cache/pip"} }

func (s *PythonStrategy) ValidateStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("validate-workspace", "python:3.12-alpine", resourcesTiny(), scriptValidatePython(apps))
}

func (s *PythonStrategy) InstallStep(apps []services.AppSpec) *forge.Container {
	c := buildContainer("install-deps", "python:3.12-alpine", resourcesMedium(), scriptInstallPython(apps))
	c.RetryStrategy = retryStrategy(3, "5s", "2", "60s")
	return c
}

func (s *PythonStrategy) BuildStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", "python:3.12-alpine", resourcesSmall(), scriptBuildPython(apps))
}

func (s *PythonStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", "python:3.12-alpine", resourcesSmall(), scriptScanPython())
}

func (s *PythonStrategy) DockerfileStep(apps []services.AppSpec) *forge.Container {
	return buildContainer("generate-dockerfile", "python:3.12-alpine", resourcesSmall(), scriptDockerfilePython(apps))
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
	return buildContainer("validate-workspace", "alpine:3.19.1", resourcesTiny(), scriptValidateCustom())
}

func (s *CustomStrategy) InstallStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("install-deps", "alpine:3.19.1", resourcesTiny(), noopScript("No install step for custom framework — user manages dependencies in Dockerfile"))
}

func (s *CustomStrategy) BuildStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("build-apps", "alpine:3.19.1", resourcesTiny(), noopScript("No build step for custom framework — build happens in Dockerfile"))
}

func (s *CustomStrategy) ScanStep() *forge.Container {
	return buildContainer("security-scan", "alpine:3.19.1", resourcesTiny(), noopScript("No security scan for custom framework"))
}

func (s *CustomStrategy) DockerfileStep(_ []services.AppSpec) *forge.Container {
	return buildContainer("generate-dockerfile", "alpine:3.19.1", resourcesTiny(), scriptValidateCustom())
}

// ---------------------------------------------------------------------------
// Helper: forge container builder
// ---------------------------------------------------------------------------

func ptr[T any](v T) *T { return &v }

func buildContainer(name, image string, resources *forge.ResourceRequirements, script string) *forge.Container {
	return &forge.Container{
		Name:    name,
		Image:   image,
		Command: []string{"sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "workspace", MountPath: "/workspace"}},
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "manifests", MountPath: "/manifests"}},
		},
		Resources: resources,
		SecurityContext: &model.SecurityContext{
			RunAsNonRoot: ptr(true),
			RunAsUser:    ptr(int64(1000)),
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

// ---------------------------------------------------------------------------
// Node.js scripts (extracted from monolithic YAML)
// ---------------------------------------------------------------------------

func scriptValidateNode(apps []services.AppSpec) string {
	// Injects app paths at build time so the shell script doesn't need Node.js to parse JSON.
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
  mkdir -p /manifests
  echo "validate-workspace: $ERRORS" > /manifests/build-error.txt
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
  npm audit --audit-level=high 2>&1 || {
    echo "WARNING: npm audit found high-severity vulnerabilities"
    echo "This is a warning only for v1 — build continues"
  }
else
  echo "No package-lock.json found — skipping npm audit"
fi
`
}

func scriptDockerfileNode(apps []services.AppSpec) string {
	port := defaultPort(apps, 3000)
	appPath := "."
	if len(apps) > 0 && apps[0].Path != "" && apps[0].Path != "." {
		appPath = apps[0].Path
	}

	// For monorepo apps (path != "."), the Dockerfile sets WORKDIR to the app subdir
	// so that npm ci finds package.json in the right place. COPY . . still copies the
	// entire workspace (which Kaniko uses as context), then WORKDIR navigates to the app.
	workdir := "/app"
	if appPath != "." {
		workdir = fmt.Sprintf("/app/%s", appPath)
	}

	return fmt.Sprintf(`set -euo pipefail
cd /workspace
if [ -f Dockerfile ]; then
  echo "User-provided Dockerfile found — skipping generation"
  cat Dockerfile
  exit 0
fi
echo "No Dockerfile found — generating for Node.js"
cat > Dockerfile << 'DEOF'
FROM node:20-alpine
WORKDIR /app
COPY . .
WORKDIR %s
RUN npm ci --omit=dev 2>/dev/null || npm install --production
EXPOSE %d
CMD ["npm", "start"]
DEOF
echo "Generated Dockerfile:"
cat Dockerfile
`, workdir, port)
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
  mkdir -p /manifests
  echo "validate-workspace: $ERRORS" > /manifests/build-error.txt
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
    echo "WARNING: govulncheck found vulnerabilities"
    echo "This is a warning only for v1 — build continues"
  }
else
  echo "govulncheck not available — skipping vulnerability scan"
fi
`
}

func scriptDockerfileGo(apps []services.AppSpec) string {
	port := defaultPort(apps, 8080)
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
RUN CGO_ENABLED=0 go build -o /app ./...

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /app /app
EXPOSE %d
ENTRYPOINT ["/app"]
DEOF
echo "Generated Dockerfile:"
cat Dockerfile
`, port)
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
  mkdir -p /manifests
  echo "validate-workspace: $ERRORS" > /manifests/build-error.txt
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
    echo "WARNING: pip-audit found vulnerabilities"
    echo "This is a warning only for v1 — build continues"
  }
else
  pip install pip-audit 2>/dev/null && pip-audit 2>&1 || {
    echo "pip-audit not available or scan failed — skipping"
  }
fi
`
}

func scriptDockerfilePython(apps []services.AppSpec) string {
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
  mkdir -p /manifests
  echo "validate-workspace: Custom framework requires a Dockerfile" > /manifests/build-error.txt
  exit 1
fi
echo "Dockerfile found — custom build validated"
`
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// appPaths extracts the workspace-relative paths from app specs.
// Missing paths fall back to "." for backwards compatibility with
// clients that omit app.path in build requests.
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
	// Use the last path component
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
