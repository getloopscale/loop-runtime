// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import "testing"

func TestCreateBuildPlan_SingleApp(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: ".", Framework: "express", Port: 3000, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
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
		{Name: "api", Type: "server", Path: "apps/api", Framework: "express", Port: 3000, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
		{Name: "worker", Type: "worker", Path: "apps/worker", Framework: "node", Port: 0, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
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
		{Name: "web", Type: "server", Path: "apps/web", Framework: "custom", Port: 3000, Build: "dockerfile", Dockerfile: "Dockerfile.prod", Env: map[string]string{}, Secrets: []string{}},
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

func TestCreateBuildPlan_WithEnv(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: ".", Framework: "express", Port: 3000, Build: "buildpack",
			Env: map[string]string{"NODE_ENV": "production", "LOG_LEVEL": "info"}, Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "acme", "app", "sha1", "reg.io")

	args := plan.Apps[0].BuildArgs
	if args["NODE_ENV"] != "production" {
		t.Errorf("BuildArgs[NODE_ENV] = %q", args["NODE_ENV"])
	}
	if args["LOG_LEVEL"] != "info" {
		t.Errorf("BuildArgs[LOG_LEVEL] = %q", args["LOG_LEVEL"])
	}
	// System vars always present
	if args["COMMIT_SHA"] != "sha1" {
		t.Errorf("BuildArgs[COMMIT_SHA] = %q", args["COMMIT_SHA"])
	}
}

func TestCreateBuildPlan_GoFramework(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: ".", Framework: "gin", Port: 8080, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "t", "p", "sha", "r")

	if plan.Apps[0].LanguageStrategy != "go" {
		t.Errorf("LanguageStrategy = %q, want go", plan.Apps[0].LanguageStrategy)
	}
}

func TestCreateBuildPlan_PythonFramework(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: ".", Framework: "fastapi", Port: 8000, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "t", "p", "sha", "r")

	if plan.Apps[0].LanguageStrategy != "python" {
		t.Errorf("LanguageStrategy = %q, want python", plan.Apps[0].LanguageStrategy)
	}
}

func TestResolveLanguageStrategy_AllFrameworks(t *testing.T) {
	nodeFrameworks := []string{
		"nextjs", "next", "vite", "remix", "nestjs", "express", "node",
		"react", "vue", "svelte", "astro", "angular", "fastify",
	}
	for _, fw := range nodeFrameworks {
		if got := resolveLanguageStrategy(fw); got != "node" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want node", fw, got)
		}
	}

	goFrameworks := []string{"gin", "go"}
	for _, fw := range goFrameworks {
		if got := resolveLanguageStrategy(fw); got != "go" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want go", fw, got)
		}
	}

	pythonFrameworks := []string{"fastapi", "django", "python"}
	for _, fw := range pythonFrameworks {
		if got := resolveLanguageStrategy(fw); got != "python" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want python", fw, got)
		}
	}

	rubyFrameworks := []string{"sinatra", "rails", "ruby"}
	for _, fw := range rubyFrameworks {
		if got := resolveLanguageStrategy(fw); got != "ruby" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want ruby", fw, got)
		}
	}

	javaFrameworks := []string{"spring", "spring-boot", "java"}
	for _, fw := range javaFrameworks {
		if got := resolveLanguageStrategy(fw); got != "java" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want java", fw, got)
		}
	}

	phpFrameworks := []string{"laravel", "slim", "php"}
	for _, fw := range phpFrameworks {
		if got := resolveLanguageStrategy(fw); got != "php" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want php", fw, got)
		}
	}

	rustFrameworks := []string{"axum", "actix", "rocket", "rust"}
	for _, fw := range rustFrameworks {
		if got := resolveLanguageStrategy(fw); got != "rust" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want rust", fw, got)
		}
	}

	if got := resolveLanguageStrategy("unknown"); got != "custom" {
		t.Errorf("resolveLanguageStrategy(unknown) = %q, want custom", got)
	}
}

func TestCreateBuildPlan_MixedLanguages(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Path: "apps/api", Framework: "express", Port: 3000, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
		{Name: "worker", Type: "worker", Path: "apps/worker", Framework: "go", Port: 0, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
		{Name: "ml", Type: "worker", Path: "apps/ml", Framework: "fastapi", Port: 0, Build: "buildpack", Env: map[string]string{}, Secrets: []string{}},
	}
	plan := CreateBuildPlan(units, "acme", "mono", "sha", "reg.io")

	strategies := make(map[string]string)
	for _, app := range plan.Apps {
		strategies[app.AppName] = app.LanguageStrategy
	}

	if strategies["api"] != "node" {
		t.Errorf("api strategy = %q, want node", strategies["api"])
	}
	if strategies["worker"] != "go" {
		t.Errorf("worker strategy = %q, want go", strategies["worker"])
	}
	if strategies["ml"] != "python" {
		t.Errorf("ml strategy = %q, want python", strategies["ml"])
	}
}
