// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// build-mode-dispatch-and-dockerfile-security-plan T1.3 + T1.4 + T4.1.

// T1.1 (simplify-deploy-loopzerokit-runtime-wiring / F-dom-2): ResolveStrategyForApp
// routes by the services.json `runtime` (python|node) — the single build-strategy
// key. Only python/node resolve; empty/unknown fails closed (D1/D4).
func TestResolveStrategyForApp_RoutesByRuntime(t *testing.T) {
	cases := []struct {
		runtime string
		want    string
	}{
		{"node", "node"},
		{"python", "python"},
		{"", "unsupported"},   // empty runtime fails closed (D4), no Node default
		{"go", "unsupported"}, // non-stack runtime fails closed
	}
	for _, tc := range cases {
		t.Run(tc.runtime, func(t *testing.T) {
			app := deploycontracts.AppSpec{Name: "api", Runtime: tc.runtime, Build: ""}
			s := ResolveStrategyForApp(app)
			if s.Name() != tc.want {
				t.Errorf("runtime=%q → strategy=%q, want %q", tc.runtime, s.Name(), tc.want)
			}
		})
	}
}

func TestAllLanguageStrategiesImplementDockerfileStep(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "api", Path: ".", Start: "x"}}
	strategies := []LanguageStrategy{
		&NodeStrategy{},
		&PythonStrategy{},
		&unsupportedStrategy{runtime: "go"},
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
	apps := []deploycontracts.AppSpec{
		{Name: "api", Path: ".", Runtime: "node", Build: "", Type: "server", Port: 3000},
	}
	spec := deploycontracts.WorkflowSpec{
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
