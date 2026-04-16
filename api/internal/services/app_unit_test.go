// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import "testing"

func TestNormalizeApp_ServerExpress(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{
		Path: "apps/api", Framework: "express", Port: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server", unit.Type)
	}
	if unit.Build != "buildpack" {
		t.Errorf("Build = %q, want buildpack", unit.Build)
	}
	if unit.Port != 3000 {
		t.Errorf("Port = %d, want 3000", unit.Port)
	}
	if unit.Dockerfile != "" {
		t.Errorf("Dockerfile = %q, want empty (buildpack)", unit.Dockerfile)
	}
}

func TestNormalizeApp_FrontendNextjsStatic(t *testing.T) {
	// MEET-2026-03-29-016: Frontend MUST have render: static
	unit, err := NormalizeApp("web", AppSpec{
		Path: "apps/web", Framework: "nextjs", Rendering: "static",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "frontend" {
		t.Errorf("Type = %q, want frontend", unit.Type)
	}
	if unit.Rendering != "static" {
		t.Errorf("Rendering = %q, want static", unit.Rendering)
	}
}

func TestNormalizeApp_FrontendWithoutStaticRejected(t *testing.T) {
	// MEET-2026-03-29-016: Frontend without render: static must be rejected
	_, err := NormalizeApp("web", AppSpec{
		Path: "apps/web", Framework: "nextjs",
	})
	if err == nil {
		t.Fatal("expected error for frontend without render: static")
	}
}

func TestNormalizeApp_RemixIsServer(t *testing.T) {
	// MEET-2026-03-29-016: Remix is SSR-first, classified as server
	unit, err := NormalizeApp("web", AppSpec{
		Path: "apps/web", Framework: "remix", Port: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server (remix is SSR-first)", unit.Type)
	}
}

func TestNormalizeApp_WorkerExplicit(t *testing.T) {
	unit, err := NormalizeApp("worker", AppSpec{
		Path: "apps/worker", Framework: "node", Type: "worker", Port: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "worker" {
		t.Errorf("Type = %q, want worker", unit.Type)
	}
	if unit.Port != 0 {
		t.Errorf("Port = %d, want 0 (workers have no port)", unit.Port)
	}
}

func TestNormalizeApp_CustomDockerfile(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{
		Path: ".", Framework: "custom", Build: "dockerfile", Dockerfile: "Dockerfile.prod",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Build != "dockerfile" {
		t.Errorf("Build = %q, want dockerfile", unit.Build)
	}
	if unit.Dockerfile != "Dockerfile.prod" {
		t.Errorf("Dockerfile = %q, want Dockerfile.prod", unit.Dockerfile)
	}
}

func TestNormalizeApp_DefaultsWhenEmpty(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server (default)", unit.Type)
	}
	if unit.Path != "." {
		t.Errorf("Path = %q, want . (default)", unit.Path)
	}
	if unit.Framework != "custom" {
		t.Errorf("Framework = %q, want custom (default)", unit.Framework)
	}
	if unit.Build != "dockerfile" {
		t.Errorf("Build = %q, want dockerfile (custom framework)", unit.Build)
	}
	if unit.Dockerfile != "Dockerfile" {
		t.Errorf("Dockerfile = %q, want Dockerfile (default)", unit.Dockerfile)
	}
	if unit.Port != 8080 {
		t.Errorf("Port = %d, want 8080 (default server port)", unit.Port)
	}
	if len(unit.Env) != 0 {
		t.Errorf("Env = %v, want empty map", unit.Env)
	}
	if len(unit.Secrets) != 0 {
		t.Errorf("Secrets = %v, want empty slice", unit.Secrets)
	}
}

func TestNormalizeApp_WithEnvAndSecrets(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{
		Framework: "express",
		Port:      3000,
		Env:       map[string]string{"NODE_ENV": "production"},
		Secrets:   []string{"DATABASE_URL"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Env["NODE_ENV"] != "production" {
		t.Errorf("Env[NODE_ENV] = %q, want production", unit.Env["NODE_ENV"])
	}
	if len(unit.Secrets) != 1 || unit.Secrets[0] != "DATABASE_URL" {
		t.Errorf("Secrets = %v, want [DATABASE_URL]", unit.Secrets)
	}
}

func TestNormalizeApp_GoFramework(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{
		Framework: "gin", Port: 8080,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server", unit.Type)
	}
	if unit.Build != "buildpack" {
		t.Errorf("Build = %q, want buildpack", unit.Build)
	}
}

func TestNormalizeApp_InvalidType(t *testing.T) {
	_, err := NormalizeApp("api", AppSpec{Type: "invalid"})
	if err == nil {
		t.Error("expected error for invalid type")
	}
}
