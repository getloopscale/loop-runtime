// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"testing"

	"github.com/loopzero/loopzero/api/internal/services"
)

// build-mode-dispatch-and-dockerfile-security-plan T1.3 + T1.4 + T4.1.

func TestResolveStrategyForApp_NoBuildDockerfile_RoutesByFramework(t *testing.T) {
	cases := []struct {
		framework string
		want      string
	}{
		// T2.1: fixed node/python stack — everything else fails closed.
		{"node", "node"},
		{"express", "node"},
		{"nextjs", "node"},
		{"python", "python"},
		{"fastapi", "python"},
		{"", "node"},
		{"go", "unsupported"},
		{"ruby", "unsupported"},
		{"java", "unsupported"},
		{"php", "unsupported"},
		{"rust", "unsupported"},
		{"custom", "unsupported"},
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

func TestAllLanguageStrategiesImplementDockerfileStep(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: ".", Start: "x"}}
	strategies := []LanguageStrategy{
		&NodeStrategy{},
		&PythonStrategy{},
		&unsupportedStrategy{framework: "go"},
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

// TestWorkflowBuilder_NodeApp_EmitsPacksNotLint ensures a node app produces the
// generate-dockerfile step (now the canonical generator, name preserved) and no
// dockerfile-lint step (BYO path removed in T2.2).
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
