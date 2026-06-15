// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"testing"
)

// TestCreateBuildPlan_StaticDeliveryModel removed (simplify-deploy-loopzerokit-only
// T3.3/F-wire-2): static-frontend delivery was deleted, so DeliveryModel is
// always "container". The `frontend` type + `static` rendering it exercised no
// longer exist in the contract. TestCreateBuildPlan_ContainerDeliveryModel below
// covers the only remaining delivery model.

func TestCreateBuildPlan_ContainerDeliveryModel(t *testing.T) {
	units := []AppUnit{
		{
			Name:      "api",
			Type:      "server",
			Port:      3000,
			Path:      "apps/api",
			Framework: "express",
			Build:     "buildpack",
			Rendering: "server",
		},
	}

	plan := CreateBuildPlan(units, "tn-1", "my-app", "sha123", "registry.example.com")

	if len(plan.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(plan.Apps))
	}
	if plan.Apps[0].DeliveryModel != "container" {
		t.Errorf("expected delivery model 'container', got %q", plan.Apps[0].DeliveryModel)
	}
}

// TestCreateBuildPlan_UserDockerfileIgnored_D4_Subpath locks the D4 invariant
// for a monorepo subpath app: even with Build="dockerfile" + a user Dockerfile
// + a non-root Path, the resolved DockerfilePath is the platform-canonical
// path (single app → /workspace/Dockerfile) and the context is the whole
// workspace — never the user subpath. This is the exact vector F-dom-1 flagged.
func TestCreateBuildPlan_UserDockerfileIgnored_D4_Subpath(t *testing.T) {
	units := []AppUnit{
		{
			Name:       "api",
			Type:       "server",
			Port:       3000,
			Path:       "services/api",
			Framework:  "express",
			Build:      "dockerfile",
			Dockerfile: "Dockerfile.prod",
		},
	}

	plan := CreateBuildPlan(units, "tn-1", "my-app", "sha123", "registry.example.com")

	if plan.Apps[0].DockerfilePath != "/workspace/Dockerfile" {
		t.Errorf("DockerfilePath = %q, want /workspace/Dockerfile (user Dockerfile/subpath must be ignored — D4)", plan.Apps[0].DockerfilePath)
	}
	if plan.Apps[0].ContextPath != "/workspace" {
		t.Errorf("ContextPath = %q, want /workspace (canonical build never uses the user subpath — D4)", plan.Apps[0].ContextPath)
	}
}

func TestCreateBuildPlan_MultiApp_DockerfileNaming(t *testing.T) {
	units := []AppUnit{
		{Name: "api", Type: "server", Port: 3000, Path: ".", Framework: "express", Build: "buildpack"},
		{Name: "worker", Type: "worker", Path: ".", Framework: "node", Build: "buildpack"},
	}

	plan := CreateBuildPlan(units, "tn-1", "my-app", "sha123", "registry.example.com")

	if len(plan.Apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(plan.Apps))
	}
	// Multi-app: Dockerfile.{appName}
	if plan.Apps[0].DockerfilePath != "/workspace/Dockerfile.api" {
		t.Errorf("expected multi-app dockerfile name, got %q", plan.Apps[0].DockerfilePath)
	}
	if plan.Apps[1].DockerfilePath != "/workspace/Dockerfile.worker" {
		t.Errorf("expected multi-app dockerfile name, got %q", plan.Apps[1].DockerfilePath)
	}
}

func TestCreateBuildPlan_InvalidEnvKeySkipped(t *testing.T) {
	units := []AppUnit{
		{
			Name:      "api",
			Type:      "server",
			Port:      3000,
			Path:      ".",
			Framework: "express",
			Build:     "buildpack",
			Env: map[string]string{
				"VALID_KEY":  "ok",
				"invalid-key": "should-be-skipped",
			},
		},
	}

	plan := CreateBuildPlan(units, "tn-1", "my-app", "sha123", "registry.example.com")

	if _, ok := plan.Apps[0].BuildArgs["VALID_KEY"]; !ok {
		t.Error("expected VALID_KEY in build args")
	}
	if _, ok := plan.Apps[0].BuildArgs["invalid-key"]; ok {
		t.Error("expected invalid-key to be skipped")
	}
	// System vars should always be present
	if plan.Apps[0].BuildArgs["COMMIT_SHA"] != "sha123" {
		t.Errorf("expected COMMIT_SHA=sha123, got %q", plan.Apps[0].BuildArgs["COMMIT_SHA"])
	}
	if plan.Apps[0].BuildArgs["APP_NAME"] != "api" {
		t.Errorf("expected APP_NAME=api, got %q", plan.Apps[0].BuildArgs["APP_NAME"])
	}
}
