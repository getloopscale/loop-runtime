// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// T0.2b (plan simplify-deploy-loopzerokit-only v2.0) — thread the declared start
// command through the build-plan chain: AppSpec → AppUnit → AppBuildPlan, so
// the canonical Dockerfile generator (T1.1) reads the declared CMD instead of
// a loopzero-packs heuristic.

func TestNormalizeApp_CarriesStart(t *testing.T) {
	// Arrange — a server AppSpec with a declared start command
	raw := deploycontracts.AppSpec{Name: "api", Type: "server", Port: 8080, Runtime: "node", Start: "node dist/main.js"}
	// Act
	unit, err := deploycontracts.NormalizeApp("api", raw)
	// Assert
	if err != nil {
		t.Fatalf("NormalizeApp: unexpected error: %v", err)
	}
	if unit.Start != "node dist/main.js" {
		t.Fatalf("AppUnit.Start dropped: got %q, want %q", unit.Start, "node dist/main.js")
	}
}

func TestCreateBuildPlan_CarriesStart(t *testing.T) {
	// Arrange
	units := []deploycontracts.AppUnit{{Name: "api", Type: "server", Port: 8080, Runtime: "node", Start: "node dist/main.js"}}
	// Act
	plan := deploycontracts.CreateBuildPlan(units, "tn", "proj", "abc123", "registry.example.com")
	// Assert
	if len(plan.Apps) != 1 {
		t.Fatalf("expected 1 app plan, got %d", len(plan.Apps))
	}
	if plan.Apps[0].Start != "node dist/main.js" {
		t.Fatalf("AppBuildPlan.Start dropped: got %q, want %q", plan.Apps[0].Start, "node dist/main.js")
	}
}
