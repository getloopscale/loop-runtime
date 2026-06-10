// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"testing"
)

func TestCreateBuildPlan_StaticDeliveryModel(t *testing.T) {
	units := []AppUnit{
		{
			Name:      "web",
			Type:      "frontend",
			Path:      "apps/web",
			Framework: "react",
			Build:     "buildpack",
			Rendering: "static",
		},
	}

	plan := CreateBuildPlan(units, "tn-1", "my-app", "sha123", "registry.example.com")

	if len(plan.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(plan.Apps))
	}
	if plan.Apps[0].DeliveryModel != "static" {
		t.Errorf("expected delivery model 'static', got %q", plan.Apps[0].DeliveryModel)
	}
}

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

func TestCreateBuildPlan_UserProvidedDockerfile(t *testing.T) {
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

	if plan.Apps[0].DockerfilePath != "/workspace/services/api/Dockerfile.prod" {
		t.Errorf("expected user dockerfile path, got %q", plan.Apps[0].DockerfilePath)
	}
	if plan.Apps[0].ContextPath != "/workspace/services/api" {
		t.Errorf("expected context path for user dockerfile, got %q", plan.Apps[0].ContextPath)
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
