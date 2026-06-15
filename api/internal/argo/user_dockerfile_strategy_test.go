// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	"github.com/loopzero/loopzero/api/internal/services"
)

// build-mode-dispatch-and-dockerfile-security-plan T1.3 + T1.4 + T4.1.

func TestResolveStrategyForApp_BuildDockerfile_RoutesToUserDockerfile(t *testing.T) {
	app := services.AppSpec{Name: "api", Framework: "node", Build: "dockerfile"}
	s := ResolveStrategyForApp(app)
	if s.Name() != "user-dockerfile" {
		t.Errorf("Build=dockerfile must route to UserDockerfileStrategy, got %q", s.Name())
	}
}

func TestResolveStrategyForApp_NoBuildDockerfile_RoutesByFramework(t *testing.T) {
	cases := []struct {
		framework string
		want      string
	}{
		{"node", "node"},
		{"express", "node"},
		{"nextjs", "node"},
		{"go", "go"},
		{"gin", "go"},
		{"python", "python"},
		{"fastapi", "python"},
		{"ruby", "ruby"},
		{"java", "java"},
		{"php", "php"},
		{"rust", "rust"},
		{"custom", "custom"},
		{"", "node"},
	}
	for _, tc := range cases {
		t.Run(tc.framework, func(t *testing.T) {
			app := services.AppSpec{Name: "api", Framework: tc.framework, Build: ""}
			s := ResolveStrategyForApp(app)
			if s.Name() != tc.want {
				t.Errorf("framework=%q Build='' → strategy=%q, want %q", tc.framework, s.Name(), tc.want)
			}
		})
	}
}

func TestResolveStrategyForApp_BuildDockerfileOverridesFrameworkCustom(t *testing.T) {
	app := services.AppSpec{Name: "api", Framework: "custom", Build: "dockerfile"}
	s := ResolveStrategyForApp(app)
	if s.Name() != "user-dockerfile" {
		t.Errorf("Build=dockerfile + Framework=custom → %q, want user-dockerfile", s.Name())
	}
}

func TestUserDockerfileStrategy_LifecycleStepsAreNoOp(t *testing.T) {
	s := &UserDockerfileStrategy{}
	apps := []services.AppSpec{{Name: "api", Path: ".", Framework: "node", Build: "dockerfile"}}

	for _, tc := range []struct {
		name string
		args string
	}{
		{"validate-workspace", s.ValidateStep(apps).Args[0]},
		{"install-deps", s.InstallStep(apps).Args[0]},
		{"build-apps", s.BuildStep(apps).Args[0]},
		{"security-scan", s.ScanStep().Args[0]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.args, "user-dockerfile") && !strings.Contains(tc.args, "skip") && !strings.Contains(tc.args, "Dockerfile") {
				t.Errorf("expected no-op script for %s, got: %s", tc.name, tc.args)
			}
		})
	}
}

func TestUserDockerfileStrategy_DockerfileStepReturnsLintContainer(t *testing.T) {
	s := &UserDockerfileStrategy{}
	apps := []services.AppSpec{{Name: "api", Path: ".", Framework: "node", Build: "dockerfile"}}

	c := s.DockerfileStep(apps, 1)
	if c.Name != "dockerfile-lint" {
		t.Errorf("DockerfileStep().Name = %q, want dockerfile-lint", c.Name)
	}
	if !strings.Contains(c.Args[0], "loopzero-dockerfile-lint") {
		t.Errorf("expected loopzero-dockerfile-lint invocation in args, got: %s", c.Args[0])
	}
	if !strings.Contains(c.Args[0], "--apps") {
		t.Errorf("expected --apps flag in invocation, got: %s", c.Args[0])
	}
}

// T1.2 (plan simplify-deploy-loopzerokit-only v2.0): NodeStrategy now emits the
// canonical platform Dockerfile, not loopzero-packs. The former
// TestNodeStrategy_DockerfileStepReturnsLoopZeroPacks asserted the removed behavior
// and is superseded by TestNodeStrategy_DockerfileStep_UsesCanonical
// (languages_canonical_test.go).

func TestGoStrategy_DockerfileStepReturnsLoopZeroPacks(t *testing.T) {
	s := &GoStrategy{}
	apps := []services.AppSpec{{Name: "api", Path: ".", Framework: "go"}}
	c := s.DockerfileStep(apps, 1)
	if c.Name != "generate-dockerfile" {
		t.Errorf("Go DockerfileStep().Name = %q, want generate-dockerfile", c.Name)
	}
}

func TestAllLanguageStrategiesImplementDockerfileStep(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "."}}
	strategies := []LanguageStrategy{
		&NodeStrategy{},
		&GoStrategy{},
		&PythonStrategy{},
		&RubyStrategy{},
		&JavaStrategy{},
		&PHPStrategy{},
		&RustStrategy{},
		&CustomStrategy{},
		&UserDockerfileStrategy{},
	}
	for _, s := range strategies {
		t.Run(s.Name(), func(t *testing.T) {
			c := s.DockerfileStep(apps, 1)
			if c == nil {
				t.Errorf("DockerfileStep returned nil for strategy %s", s.Name())
				return
			}
			if c.Name == "" {
				t.Errorf("DockerfileStep().Name empty for strategy %s", s.Name())
			}
		})
	}
}

// TestWorkflowBuilder_BuildDockerfileApp_EmitsLintNotPacks ensures end-to-end
// that an app with build:dockerfile produces a workflow containing the
// dockerfile-lint step (and NOT generate-dockerfile via loopzero-packs).
func TestWorkflowBuilder_BuildDockerfileApp_EmitsLintNotPacks(t *testing.T) {
	b := NewWorkflowBuilder(InfraParams{
		RegistryURL:      "k3d-loopzero-registry:5000",
		RegistryInsecure: true,
		BaseDomain:       "loopzero.local",
	})
	apps := []services.AppSpec{
		{Name: "api", Path: ".", Framework: "node", Build: "dockerfile", Type: "server", Port: 3000},
	}
	spec := services.WorkflowSpec{
		BuildID:   "build-test",
		TenantID:  "acme",
		ProjectID: "myproj",
		Apps:      apps,
		Plan:      planFromApps(apps),
	}

	wf, err := b.Build(spec)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	hasLint := false
	hasPacks := false
	for _, tmpl := range wf.Templates {
		switch tmpl.GetName() {
		case "dockerfile-lint":
			hasLint = true
		case "generate-dockerfile":
			hasPacks = true
		}
	}
	if !hasLint {
		t.Errorf("expected dockerfile-lint template in workflow, not found")
	}
	if hasPacks {
		t.Errorf("expected NO generate-dockerfile (loopzero-packs) for build:dockerfile app, but found one")
	}
}

func TestWorkflowBuilder_NodeApp_EmitsPacksNotLint(t *testing.T) {
	b := NewWorkflowBuilder(InfraParams{
		RegistryURL:      "k3d-loopzero-registry:5000",
		RegistryInsecure: true,
		BaseDomain:       "loopzero.local",
	})
	apps := []services.AppSpec{
		{Name: "api", Path: ".", Framework: "node", Build: "", Type: "server", Port: 3000},
	}
	spec := services.WorkflowSpec{
		BuildID:   "build-test",
		TenantID:  "acme",
		ProjectID: "myproj",
		Apps:      apps,
		Plan:      planFromApps(apps),
	}

	wf, err := b.Build(spec)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	hasPacks := false
	hasLint := false
	for _, tmpl := range wf.Templates {
		switch tmpl.GetName() {
		case "generate-dockerfile":
			hasPacks = true
		case "dockerfile-lint":
			hasLint = true
		}
	}
	if !hasPacks {
		t.Errorf("expected generate-dockerfile (loopzero-packs) for node app, not found")
	}
	if hasLint {
		t.Errorf("expected NO dockerfile-lint for non-build:dockerfile node app, but found one")
	}
}

// TestWorkflowBuilder_MixedMonorepo_RoutesPerApp ensures a monorepo with
// 1 build:dockerfile app + 1 framework:node app produces BOTH steps.
func TestWorkflowBuilder_MixedMonorepo_RoutesPerApp(t *testing.T) {
	b := NewWorkflowBuilder(InfraParams{
		RegistryURL:      "k3d-loopzero-registry:5000",
		RegistryInsecure: true,
		BaseDomain:       "loopzero.local",
	})
	apps := []services.AppSpec{
		{Name: "api", Path: "apps/api", Framework: "node", Build: "dockerfile", Type: "server", Port: 3000},
		// Frontend MUST declare Rendering: "static" — NormalizeApp rejects
		// frontends without it (CDN-delivery contract). The legacy spec.Apps
		// fallback bypassed normalization; T1.3 (CR-2) deleted that bypass.
		{Name: "web", Path: "apps/web", Framework: "node", Build: "", Type: "frontend", Rendering: "static"},
	}
	spec := services.WorkflowSpec{
		BuildID:   "build-test",
		TenantID:  "acme",
		ProjectID: "myproj",
		Apps:      apps,
		Plan:      planFromApps(apps),
	}

	wf, err := b.Build(spec)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	hasLint := false
	hasPacks := false
	for _, tmpl := range wf.Templates {
		name := tmpl.GetName()
		// Multi-language groups get a suffix; e.g., "generate-dockerfile-node"
		if strings.HasPrefix(name, "dockerfile-lint") {
			hasLint = true
		}
		if strings.HasPrefix(name, "generate-dockerfile") {
			hasPacks = true
		}
	}
	if !hasLint {
		t.Errorf("expected dockerfile-lint (for build:dockerfile app) in mixed workflow, not found")
	}
	if !hasPacks {
		t.Errorf("expected generate-dockerfile (for node app) in mixed workflow, not found")
	}
}
