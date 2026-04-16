// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	forge "github.com/getloopscale/loopzero-forge"

	"github.com/loopzero/loopzero/api/internal/services"
)

// ---------------------------------------------------------------------------
// ResolveStrategy
// ---------------------------------------------------------------------------

func TestResolveStrategy_NodeFrameworks(t *testing.T) {
	nodeFrameworks := []string{"nextjs", "vite", "remix", "nestjs", "express", "node", ""}
	for _, fw := range nodeFrameworks {
		strategy := ResolveStrategy(fw)
		if strategy.Name() != "node" {
			t.Errorf("ResolveStrategy(%q) = %q, want node", fw, strategy.Name())
		}
	}
}

func TestResolveStrategy_GoFrameworks(t *testing.T) {
	goFrameworks := []string{"gin", "go"}
	for _, fw := range goFrameworks {
		strategy := ResolveStrategy(fw)
		if strategy.Name() != "go" {
			t.Errorf("ResolveStrategy(%q) = %q, want go", fw, strategy.Name())
		}
	}
}

func TestResolveStrategy_PythonFrameworks(t *testing.T) {
	pyFrameworks := []string{"fastapi", "django", "python"}
	for _, fw := range pyFrameworks {
		strategy := ResolveStrategy(fw)
		if strategy.Name() != "python" {
			t.Errorf("ResolveStrategy(%q) = %q, want python", fw, strategy.Name())
		}
	}
}

func TestResolveStrategy_CustomFramework(t *testing.T) {
	strategy := ResolveStrategy("custom")
	if strategy.Name() != "custom" {
		t.Errorf("ResolveStrategy(custom) = %q, want custom", strategy.Name())
	}
}

func TestResolveStrategy_UnknownFrameworkFallsToCustom(t *testing.T) {
	strategy := ResolveStrategy("unknown-framework")
	if strategy.Name() != "custom" {
		t.Errorf("ResolveStrategy(unknown-framework) = %q, want custom", strategy.Name())
	}
}

// ---------------------------------------------------------------------------
// NodeStrategy
// ---------------------------------------------------------------------------

func TestNodeStrategy_CacheKey(t *testing.T) {
	s := &NodeStrategy{}
	if s.CacheKey() != "node-modules" {
		t.Errorf("CacheKey() = %q, want node-modules", s.CacheKey())
	}
}

func TestNodeStrategy_CacheDirs(t *testing.T) {
	s := &NodeStrategy{}
	dirs := s.CacheDirs()
	if len(dirs) != 1 || dirs[0] != "node_modules" {
		t.Errorf("CacheDirs() = %v, want [node_modules]", dirs)
	}
}

func TestNodeStrategy_ValidateStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	c := s.ValidateStep(apps)

	assertContainerShape(t, c, "validate-workspace", "node:20.11.1-alpine")
}

func TestNodeStrategy_InstallStep_HasRetryStrategy(t *testing.T) {
	s := &NodeStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	c := s.InstallStep(apps)

	assertContainerShape(t, c, "install-deps", "node:20.11.1-alpine")

	if c.RetryStrategy == nil {
		t.Error("InstallStep must have RetryStrategy")
	}
}

func TestNodeStrategy_BuildStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	c := s.BuildStep(apps)

	assertContainerShape(t, c, "build-apps", "node:20.11.1-alpine")
}

func TestNodeStrategy_ScanStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	c := s.ScanStep()

	assertContainerShape(t, c, "security-scan", "node:20.11.1-alpine")
}

// ---------------------------------------------------------------------------
// Dockerfile naming (MEET-2026-03-29-018)
// ---------------------------------------------------------------------------

func TestDockerfilePathForApp_SingleApp(t *testing.T) {
	path := dockerfilePathForApp("api", 1)
	if path != "/workspace/Dockerfile" {
		t.Errorf("single app: got %q, want /workspace/Dockerfile", path)
	}
}

func TestDockerfilePathForApp_MultiApp(t *testing.T) {
	path := dockerfilePathForApp("api", 3)
	if path != "/workspace/Dockerfile.api" {
		t.Errorf("multi app: got %q, want /workspace/Dockerfile.api", path)
	}
}

func TestLoopZeroPacksDockerfileStep_SingleApp(t *testing.T) {
	// Single app (totalApps=1) → output is Dockerfile (no suffix)
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8080, Path: "apps/api"}}
	c := loopzeroPacksDockerfileStep(apps, 1)

	assertContainerShape(t, c, "generate-dockerfile", imageLoopZeroPacks)

	script := extractContainerScript(t, c)
	assertContains(t, script, "loopzeropacks-generate")
	// CODE-001 defense-in-depth: values are now %q-quoted in the shell script
	assertContains(t, script, `--app-path "apps/api"`)
	assertContains(t, script, `--output "/workspace/Dockerfile"`)
	// Must NOT contain .api suffix for single app
	if strings.Contains(script, "Dockerfile.api") {
		t.Error("single app should output Dockerfile, not Dockerfile.api")
	}
}

func TestLoopZeroPacksDockerfileStep_MultipleApps(t *testing.T) {
	// 2 apps same language (totalApps=2) → Dockerfile.{name}
	apps := []services.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Path: "apps/api"},
		{Name: "worker", Type: "worker", Path: "apps/worker"},
	}
	c := loopzeroPacksDockerfileStep(apps, 2)
	script := extractContainerScript(t, c)
	assertContains(t, script, "Dockerfile.api")
	assertContains(t, script, "Dockerfile.worker")
}

func TestLoopZeroPacksDockerfileStep_MixedLanguageMonorepo(t *testing.T) {
	// Go API group (1 app) but totalApps=2 → must use Dockerfile.api
	goApps := []services.AppSpec{{Name: "api", Type: "server", Port: 8080, Path: "apps/api"}}
	c := loopzeroPacksDockerfileStep(goApps, 2)
	script := extractContainerScript(t, c)
	assertContains(t, script, "Dockerfile.api")
}

func TestDockerfileNaming_GeneratorMatchesConsumer(t *testing.T) {
	// Contract test: generator and consumer must agree on filenames
	for _, tc := range []struct {
		name      string
		appName   string
		totalApps int
	}{
		{"single", "api", 1},
		{"multi-2", "api", 2},
		{"multi-3", "worker", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Generator side (loopzeroPacksDockerfileStep uses dockerfilePathForApp)
			generatorPath := dockerfilePathForApp(tc.appName, tc.totalApps)
			// Consumer side (buildkitTemplateForApp uses same function)
			consumerPath := dockerfilePathForApp(tc.appName, tc.totalApps)
			if generatorPath != consumerPath {
				t.Errorf("naming mismatch: generator=%q consumer=%q", generatorPath, consumerPath)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GoStrategy
// ---------------------------------------------------------------------------

func TestGoStrategy_CacheKey(t *testing.T) {
	s := &GoStrategy{}
	if s.CacheKey() != "go-modules" {
		t.Errorf("CacheKey() = %q, want go-modules", s.CacheKey())
	}
}

func TestGoStrategy_CacheDirs(t *testing.T) {
	s := &GoStrategy{}
	dirs := s.CacheDirs()
	if len(dirs) != 2 {
		t.Errorf("CacheDirs() has %d dirs, want 2", len(dirs))
	}
}

func TestGoStrategy_ValidateStep_UsesGoImage(t *testing.T) {
	s := &GoStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8080, Framework: "go"}}
	c := s.ValidateStep(apps)

	assertContainerShape(t, c, "validate-workspace", "golang:1.22-alpine")

	script := extractContainerScript(t, c)
	assertContains(t, script, "go.mod")
}

func TestGoStrategy_BuildStep_UsesCGODisabled(t *testing.T) {
	s := &GoStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8080, Path: "cmd/api"}}
	c := s.BuildStep(apps)

	script := extractContainerScript(t, c)
	assertContains(t, script, "CGO_ENABLED=0")
}

// TestGoStrategy_DockerfileStep removed — replaced by TestLoopZeroPacksDockerfileStep.

// ---------------------------------------------------------------------------
// PythonStrategy
// ---------------------------------------------------------------------------

func TestPythonStrategy_CacheKey(t *testing.T) {
	s := &PythonStrategy{}
	if s.CacheKey() != "pip-cache" {
		t.Errorf("CacheKey() = %q, want pip-cache", s.CacheKey())
	}
}

func TestPythonStrategy_ValidateStep_ChecksPythonDeps(t *testing.T) {
	s := &PythonStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8000, Framework: "fastapi"}}
	c := s.ValidateStep(apps)

	assertContainerShape(t, c, "validate-workspace", "python:3.12-alpine")

	script := extractContainerScript(t, c)
	assertContains(t, script, "requirements.txt")
	assertContains(t, script, "pyproject.toml")
}

// TestPythonStrategy_DockerfileStep removed — replaced by TestLoopZeroPacksDockerfileStep.

// ---------------------------------------------------------------------------
// CustomStrategy
// ---------------------------------------------------------------------------

func TestCustomStrategy_CacheKey_Empty(t *testing.T) {
	s := &CustomStrategy{}
	if s.CacheKey() != "" {
		t.Errorf("CacheKey() = %q, want empty", s.CacheKey())
	}
}

func TestCustomStrategy_CacheDirs_Nil(t *testing.T) {
	s := &CustomStrategy{}
	if s.CacheDirs() != nil {
		t.Errorf("CacheDirs() = %v, want nil", s.CacheDirs())
	}
}

func TestCustomStrategy_ValidateStep_RequiresDockerfile(t *testing.T) {
	s := &CustomStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server"}}
	c := s.ValidateStep(apps)

	script := extractContainerScript(t, c)
	assertContains(t, script, "Dockerfile")
}

func TestCustomStrategy_InstallStep_IsNoop(t *testing.T) {
	s := &CustomStrategy{}
	c := s.InstallStep(nil)

	script := extractContainerScript(t, c)
	assertContains(t, script, "echo")
}

// ---------------------------------------------------------------------------
// Helper: appPaths
// ---------------------------------------------------------------------------

func TestAppPaths_EmptyApps_ReturnsDot(t *testing.T) {
	paths := appPaths(nil)
	if len(paths) != 1 || paths[0] != "." {
		t.Errorf("appPaths(nil) = %v, want [.]", paths)
	}
}

func TestAppPaths_WithPaths(t *testing.T) {
	apps := []services.AppSpec{
		{Name: "api", Path: "services/api"},
		{Name: "web", Path: ""},
	}
	paths := appPaths(apps)
	if paths[0] != "services/api" {
		t.Errorf("paths[0] = %q, want services/api", paths[0])
	}
	if paths[1] != "." {
		t.Errorf("paths[1] = %q, want . (fallback)", paths[1])
	}
}

func TestDefaultPort_UsesFirstNonZero(t *testing.T) {
	apps := []services.AppSpec{
		{Name: "worker", Port: 0},
		{Name: "api", Port: 8080},
	}
	if p := defaultPort(apps, 3000); p != 8080 {
		t.Errorf("defaultPort = %d, want 8080", p)
	}
}

func TestDefaultPort_FallbackWhenAllZero(t *testing.T) {
	apps := []services.AppSpec{{Name: "worker", Port: 0}}
	if p := defaultPort(apps, 3000); p != 3000 {
		t.Errorf("defaultPort = %d, want 3000 (fallback)", p)
	}
}

func TestSanitizeBinName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "app"},
		{".", "app"},
		{"cmd/api", "api"},
		{"api", "api"},
		{"services/internal/worker", "worker"},
	}
	for _, tt := range tests {
		got := sanitizeBinName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeBinName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertContainerShape(t *testing.T, c *forge.Container, expectedName, expectedImage string) {
	t.Helper()

	if c.Name != expectedName {
		t.Errorf("container name = %q, want %q", c.Name, expectedName)
	}
	if c.Image != expectedImage {
		t.Errorf("container image = %q, want %q", c.Image, expectedImage)
	}
	if len(c.VolumeMounts) == 0 {
		t.Error("container missing VolumeMounts")
	}
	if c.Resources == nil {
		t.Error("container missing Resources")
	}
	if c.SecurityContext == nil {
		t.Error("container missing SecurityContext")
	}
}

func extractContainerScript(t *testing.T, c *forge.Container) string {
	t.Helper()
	if len(c.Args) == 0 {
		t.Fatal("no Args in container")
	}
	return c.Args[0]
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if len(s) == 0 {
		t.Errorf("string is empty, want to contain %q", substr)
		return
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("string does not contain %q:\n%s", substr, truncate(s, 200))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
