package argo

import (
	"testing"

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
	tmpl := s.ValidateStep(apps)

	assertTemplateShape(t, tmpl, "validate-workspace", "node:20.11.1-alpine")
}

func TestNodeStrategy_InstallStep_HasRetryStrategy(t *testing.T) {
	s := &NodeStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	tmpl := s.InstallStep(apps)

	assertTemplateShape(t, tmpl, "install-deps", "node:20.11.1-alpine")

	if _, ok := tmpl["retryStrategy"]; !ok {
		t.Error("InstallStep must have retryStrategy")
	}
}

func TestNodeStrategy_BuildStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	tmpl := s.BuildStep(apps)

	assertTemplateShape(t, tmpl, "build-apps", "node:20.11.1-alpine")
}

func TestNodeStrategy_ScanStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	tmpl := s.ScanStep()

	assertTemplateShape(t, tmpl, "security-scan", "node:20.11.1-alpine")
}

func TestNodeStrategy_DockerfileStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8080}}
	tmpl := s.DockerfileStep(apps)

	assertTemplateShape(t, tmpl, "generate-dockerfile", "node:20.11.1-alpine")

	// Verify the script contains the correct port
	script := extractScript(t, tmpl)
	if script == "" {
		t.Fatal("expected non-empty script")
	}
	assertContains(t, script, "EXPOSE 8080")
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
	tmpl := s.ValidateStep(apps)

	assertTemplateShape(t, tmpl, "validate-workspace", "golang:1.22-alpine")

	script := extractScript(t, tmpl)
	assertContains(t, script, "go.mod")
}

func TestGoStrategy_BuildStep_UsesCGODisabled(t *testing.T) {
	s := &GoStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8080, Path: "cmd/api"}}
	tmpl := s.BuildStep(apps)

	script := extractScript(t, tmpl)
	assertContains(t, script, "CGO_ENABLED=0")
}

func TestGoStrategy_DockerfileStep_MultiStage(t *testing.T) {
	s := &GoStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8080}}
	tmpl := s.DockerfileStep(apps)

	script := extractScript(t, tmpl)
	assertContains(t, script, "FROM golang:1.22-alpine AS builder")
	assertContains(t, script, "EXPOSE 8080")
}

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
	tmpl := s.ValidateStep(apps)

	assertTemplateShape(t, tmpl, "validate-workspace", "python:3.12-alpine")

	script := extractScript(t, tmpl)
	assertContains(t, script, "requirements.txt")
	assertContains(t, script, "pyproject.toml")
}

func TestPythonStrategy_DockerfileStep_UsesUvicorn(t *testing.T) {
	s := &PythonStrategy{}
	apps := []services.AppSpec{{Name: "api", Type: "server", Port: 8000}}
	tmpl := s.DockerfileStep(apps)

	script := extractScript(t, tmpl)
	assertContains(t, script, "uvicorn")
	assertContains(t, script, "EXPOSE 8000")
}

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
	tmpl := s.ValidateStep(apps)

	script := extractScript(t, tmpl)
	assertContains(t, script, "Dockerfile")
}

func TestCustomStrategy_InstallStep_IsNoop(t *testing.T) {
	s := &CustomStrategy{}
	tmpl := s.InstallStep(nil)

	script := extractScript(t, tmpl)
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

func assertTemplateShape(t *testing.T, tmpl map[string]interface{}, expectedName, expectedImage string) {
	t.Helper()

	name, ok := tmpl["name"].(string)
	if !ok || name != expectedName {
		t.Errorf("template name = %v, want %q", tmpl["name"], expectedName)
	}

	container, ok := tmpl["container"].(map[string]interface{})
	if !ok {
		t.Fatal("template missing container field")
	}

	image, ok := container["image"].(string)
	if !ok || image != expectedImage {
		t.Errorf("container image = %v, want %q", container["image"], expectedImage)
	}

	// Must have volumeMounts
	if _, ok := container["volumeMounts"]; !ok {
		t.Error("container missing volumeMounts")
	}

	// Must have resources
	if _, ok := container["resources"]; !ok {
		t.Error("container missing resources")
	}

	// Must have securityContext
	if _, ok := container["securityContext"]; !ok {
		t.Error("container missing securityContext")
	}
}

func extractScript(t *testing.T, tmpl map[string]interface{}) string {
	t.Helper()
	container, ok := tmpl["container"].(map[string]interface{})
	if !ok {
		t.Fatal("no container in template")
	}
	args, ok := container["args"].([]interface{})
	if !ok || len(args) == 0 {
		t.Fatal("no args in container")
	}
	script, ok := args[0].(string)
	if !ok {
		t.Fatal("args[0] is not a string")
	}
	return script
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
