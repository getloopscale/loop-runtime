// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// simplify-deploy-loopzerokit-runtime-wiring T1.1 (closes review F-dom-2):
// end-to-end proof that the declared services.json `runtime` drives the build
// strategy through the real production chain — NormalizeApp -> CreateBuildPlan ->
// (workflow AppSpec) -> ResolveStrategyForApp — for BOTH python and node, and
// that a populated-but-stale `framework` does NOT override the runtime.

package integration

import (
	"testing"

	"github.com/loopzero/loopzero/api/internal/argo"
	"github.com/loopzero/loopzero/api/internal/services"
)

func TestRuntimeStrategyEndToEnd(t *testing.T) {
	cases := []struct {
		name        string
		runtime     string
		staleFW     string // a framework value that MUST be ignored
		wantStrat   string
	}{
		{"python app", "python", "", "python"},
		{"node app", "node", "", "node"},
		{"python app with stale framework=express (F-dom-2 fix)", "python", "express", "python"},
		{"node app with stale framework=fastapi (F-dom-2 fix)", "node", "fastapi", "node"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 1. services.json app (runtime-declared) → normalize
			raw := services.AppSpec{
				Name: "api", Type: "server", Port: 8080,
				Runtime: tc.runtime, Framework: tc.staleFW, Start: "run-me",
			}
			unit, err := services.NormalizeApp(raw.Name, raw)
			if err != nil {
				t.Fatalf("NormalizeApp: %v", err)
			}
			if unit.Runtime != tc.runtime {
				t.Fatalf("AppUnit.Runtime = %q, want %q (T0.1 wiring)", unit.Runtime, tc.runtime)
			}

			// 2. real build-plan pipeline
			plan := services.CreateBuildPlan([]services.AppUnit{unit}, "tn", "proj", "sha", "reg.io")
			if len(plan.Apps) != 1 {
				t.Fatalf("expected 1 app plan, got %d", len(plan.Apps))
			}
			if plan.Apps[0].Runtime != tc.runtime {
				t.Fatalf("AppBuildPlan.Runtime = %q, want %q (T0.1 wiring carries to plan)", plan.Apps[0].Runtime, tc.runtime)
			}

			// 3. workflow AppSpec is constructed from the plan exactly as
			//    argo.WorkflowBuilder does (Runtime carried from the plan), then
			//    the strategy is resolved by runtime — the F-dom-2 assertion.
			p := plan.Apps[0]
			wfApp := services.AppSpec{
				Name: p.AppName, Type: p.AppType, Port: p.Port,
				Path: p.AppPath, Framework: p.LanguageStrategy, Runtime: p.Runtime,
				Build: p.Build, Start: p.Start,
			}
			got := argo.ResolveStrategyForApp(wfApp).Name()
			if got != tc.wantStrat {
				t.Errorf("ResolveStrategyForApp = %q, want %q (runtime %q must drive the build strategy regardless of framework %q)",
					got, tc.wantStrat, tc.runtime, tc.staleFW)
			}
		})
	}
}

// TestRuntimeStrategyEndToEnd_EmptyRuntimeFailsClosed: an app reaching the build
// with no runtime fails closed (D4) — never a silent Node default.
func TestRuntimeStrategyEndToEnd_EmptyRuntimeFailsClosed(t *testing.T) {
	raw := services.AppSpec{Name: "api", Type: "server", Port: 8080, Start: "run-me"} // no runtime
	unit, err := services.NormalizeApp(raw.Name, raw)
	if err != nil {
		t.Fatalf("NormalizeApp: %v", err)
	}
	plan := services.CreateBuildPlan([]services.AppUnit{unit}, "tn", "proj", "sha", "reg.io")
	p := plan.Apps[0]
	wfApp := services.AppSpec{Name: p.AppName, Type: p.AppType, Port: p.Port, Runtime: p.Runtime}
	if got := argo.ResolveStrategyForApp(wfApp).Name(); got != "unsupported" {
		t.Errorf("empty-runtime strategy = %q, want unsupported (D4 — no Node default)", got)
	}
}
