// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import (
	"testing"
)

func TestCreateBuildPlan_SingleApp(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: ".", Runtime: "node", Port: 3000, Build: "buildpack", Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "acme", "my-app", "abc123", "registry.do.com")

	if len(plan.Apps) != 1 {
		t.Fatalf("expected 1 app plan, got %d", len(plan.Apps))
	}

	app := plan.Apps[0]
	if app.ImageTag != "registry.do.com/acme/my-app-api:abc123" {
		t.Errorf("ImageTag = %q", app.ImageTag)
	}
	if app.DockerfilePath != "/workspace/Dockerfile" {
		t.Errorf("DockerfilePath = %q, want /workspace/Dockerfile (single app)", app.DockerfilePath)
	}
	if app.ContextPath != "/workspace" {
		t.Errorf("ContextPath = %q, want /workspace", app.ContextPath)
	}
	if app.LanguageStrategy != "node" {
		t.Errorf("LanguageStrategy = %q, want node", app.LanguageStrategy)
	}
	if app.CacheRef != "registry.do.com/acme/my-app-api:buildcache" {
		t.Errorf("CacheRef = %q", app.CacheRef)
	}
	if app.BuildArgs["COMMIT_SHA"] != "abc123" {
		t.Errorf("BuildArgs[COMMIT_SHA] = %q", app.BuildArgs["COMMIT_SHA"])
	}
	if app.BuildArgs["APP_NAME"] != "api" {
		t.Errorf("BuildArgs[APP_NAME] = %q", app.BuildArgs["APP_NAME"])
	}
}

func TestCreateBuildPlan_MultiApp_Monorepo(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: "apps/api", Runtime: "node", Port: 3000, Build: "buildpack", Secrets: []string{}},
		{Name: "worker", Type: "worker", Path: "apps/worker", Runtime: "node", Port: 0, Build: "buildpack", Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "acme", "monorepo", "def456", "reg.io")

	if len(plan.Apps) != 2 {
		t.Fatalf("expected 2 app plans, got %d", len(plan.Apps))
	}

	api := plan.Apps[0]
	worker := plan.Apps[1]

	// Multi-app: Dockerfile.{appName}
	if api.DockerfilePath != "/workspace/Dockerfile.api" {
		t.Errorf("api DockerfilePath = %q, want /workspace/Dockerfile.api", api.DockerfilePath)
	}
	if worker.DockerfilePath != "/workspace/Dockerfile.worker" {
		t.Errorf("worker DockerfilePath = %q, want /workspace/Dockerfile.worker", worker.DockerfilePath)
	}

	// Context for buildpack apps is always /workspace (Dockerfile does COPY . . + WORKDIR)
	if api.ContextPath != "/workspace" {
		t.Errorf("api ContextPath = %q, want /workspace (buildpack uses full workspace as context)", api.ContextPath)
	}
}

// TestCreateBuildPlan_UserDockerfileIgnored_D4 locks the D4 security invariant
// (plan simplify-deploy-loopzerokit-only): BYO user-Dockerfile is deleted, so a
// user-supplied Build="dockerfile" + Dockerfile + Path MUST NOT influence the
// resolved DockerfilePath. The path is ALWAYS the platform-canonical location
// (matching argo.dockerfilePathForApp) so BuildKit can never be pointed at a
// user-controlled Dockerfile. Given a monorepo subpath app declaring a custom
// Dockerfile, When the plan is created, Then the Dockerfile path is the flat
// canonical /workspace/Dockerfile and the context is the whole workspace.
func TestCreateBuildPlan_UserDockerfileIgnored_D4(t *testing.T) {
	units := []AppUnit{
		{Name: "web", Type: "server", Path: "apps/web", Runtime: "node", Port: 3000, Build: "dockerfile", Dockerfile: "Dockerfile.prod", Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "acme", "app", "sha1", "reg.io")

	app := plan.Apps[0]
	// D4: user path/dockerfile IGNORED — canonical flat path for a single app.
	if app.DockerfilePath != "/workspace/Dockerfile" {
		t.Errorf("DockerfilePath = %q, want /workspace/Dockerfile (user Dockerfile/path must be ignored — D4)", app.DockerfilePath)
	}
	// Context is always the whole workspace, never the user subpath.
	if app.ContextPath != "/workspace" {
		t.Errorf("ContextPath = %q, want /workspace (canonical build never uses the user subpath — D4)", app.ContextPath)
	}
}

func TestCreateBuildPlan_PythonRuntime(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: ".", Runtime: "python", Port: 8000, Build: "buildpack", Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "t", "p", "sha", "r")

	if plan.Apps[0].LanguageStrategy != "python" {
		t.Errorf("LanguageStrategy = %q, want python", plan.Apps[0].LanguageStrategy)
	}
}

// TestResolveLanguageStrategy_AllFrameworks removed — see runtime_compat_test.go
// (TestResolveLanguageStrategy_RuntimePython/Node + _NonRuntime_Unsupported).
// The fixed LoopZeroKit stack is python|node; go/ruby/java/php/rust dispatch was
// deleted (simplify-deploy-loopzerokit-runtime-wiring T2.1 / F-dom-2).

// TestCreateBuildPlan_MixedRuntimes: a monorepo mixing the two supported
// runtimes (node + python) resolves each app's strategy by its declared runtime.
func TestCreateBuildPlan_MixedRuntimes(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: "apps/api", Runtime: "node", Port: 3000, Build: "buildpack", Secrets: []string{}},
		{Name: "ml", Type: "worker", Path: "apps/ml", Runtime: "python", Port: 0, Build: "buildpack", Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "acme", "mono", "sha", "reg.io")

	strategies := make(map[string]string)
	for _, app := range plan.Apps {
		strategies[app.AppName] = app.LanguageStrategy
	}
	if strategies["api"] != "node" {
		t.Errorf("api strategy = %q, want node", strategies["api"])
	}
	if strategies["ml"] != "python" {
		t.Errorf("ml strategy = %q, want python", strategies["ml"])
	}
}
