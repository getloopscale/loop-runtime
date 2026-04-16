// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateLoopZeroYAML_BasicValid(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: apps/api
    framework: express
`
	cfg, names, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "my-app" {
		t.Errorf("Name = %q, want my-app", cfg.Name)
	}
	if len(names) != 1 || names[0] != "api" {
		t.Errorf("names = %v, want [api]", names)
	}
}

func TestValidateLoopZeroYAML_Empty(t *testing.T) {
	_, _, err := ValidateLoopZeroYAML("")
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestValidateLoopZeroYAML_InvalidType(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: invalid
    port: 3000
    path: .
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestValidateLoopZeroYAML_ServerNeedsPort(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    path: .
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Error("expected error for server without port")
	}
}

func TestValidateLoopZeroYAML_WorkerNoPort(t *testing.T) {
	content := `
project: my-app
apps:
  worker:
    type: worker
    path: .
    framework: node
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("worker should not require port: %v", err)
	}
}

// ---------------------------------------------------------------------------
// v2 schema fields
// ---------------------------------------------------------------------------

func TestValidateLoopZeroYAML_V2_Version(t *testing.T) {
	content := `
version: 1
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
}

func TestValidateLoopZeroYAML_V2_InvalidVersion(t *testing.T) {
	content := `
version: 99
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Error("expected error for unsupported version")
	}
}

func TestValidateLoopZeroYAML_V2_BuildStrategy(t *testing.T) {
	content := `
version: 1
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    build: dockerfile
    dockerfile: Dockerfile.prod
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Apps["api"].Build != "dockerfile" {
		t.Errorf("Build = %q, want dockerfile", cfg.Apps["api"].Build)
	}
	if cfg.Apps["api"].Dockerfile != "Dockerfile.prod" {
		t.Errorf("Dockerfile = %q, want Dockerfile.prod", cfg.Apps["api"].Dockerfile)
	}
}

func TestValidateLoopZeroYAML_V2_InvalidBuildStrategy(t *testing.T) {
	content := `
version: 1
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    build: invalid
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Error("expected error for invalid build strategy")
	}
}

func TestValidateLoopZeroYAML_V2_Env(t *testing.T) {
	content := `
version: 1
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    env:
      NODE_ENV: production
      LOG_LEVEL: info
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Apps["api"].Env["NODE_ENV"] != "production" {
		t.Errorf("Env[NODE_ENV] = %q, want production", cfg.Apps["api"].Env["NODE_ENV"])
	}
}

func TestValidateLoopZeroYAML_V2_InvalidEnvKey(t *testing.T) {
	content := `
version: 1
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    env:
      invalid-key: value
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Error("expected error for invalid env key")
	}
}

func TestValidateLoopZeroYAML_V2_Ignore(t *testing.T) {
	content := `
version: 1
project: my-app
ignore:
  - node_modules
  - .git
apps:
  api:
    type: server
    port: 3000
    path: .
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Ignore) != 2 {
		t.Errorf("Ignore = %v, want 2 entries", cfg.Ignore)
	}
}

func TestValidateLoopZeroYAML_V2_Roundtrip(t *testing.T) {
	original := LoopZeroYAML{
		Version: 1,
		Project: "roundtrip-test",
		Ignore:  []string{"*.tmp"},
		Apps: map[string]AppConfig{
			"api": {
				Type:      "server",
				Port:      3000,
				Path:      "apps/api",
				Framework: "express",
				Build:     "buildpack",
				Env:       map[string]string{"NODE_ENV": "production"},
				Secrets:   []string{"DATABASE_URL"},
			},
			"worker": {
				Type:       "worker",
				Path:       "apps/worker",
				Framework:  "node",
				Build:      "dockerfile",
				Dockerfile: "Dockerfile.worker",
			},
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Validate the marshaled YAML
	cfg, names, err := ValidateLoopZeroYAML(string(data))
	if err != nil {
		t.Fatalf("ValidateLoopZeroYAML error: %v", err)
	}

	// Verify roundtrip
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "*.tmp" {
		t.Errorf("Ignore = %v, want [*.tmp]", cfg.Ignore)
	}
	if len(names) != 2 {
		t.Errorf("names = %v, want 2 apps", names)
	}

	api := cfg.Apps["api"]
	if api.Build != "buildpack" {
		t.Errorf("api.Build = %q, want buildpack", api.Build)
	}
	if api.Env["NODE_ENV"] != "production" {
		t.Errorf("api.Env[NODE_ENV] = %q, want production", api.Env["NODE_ENV"])
	}
	if len(api.Secrets) != 1 || api.Secrets[0] != "DATABASE_URL" {
		t.Errorf("api.Secrets = %v, want [DATABASE_URL]", api.Secrets)
	}

	worker := cfg.Apps["worker"]
	if worker.Build != "dockerfile" {
		t.Errorf("worker.Build = %q, want dockerfile", worker.Build)
	}
	if worker.Dockerfile != "Dockerfile.worker" {
		t.Errorf("worker.Dockerfile = %q, want Dockerfile.worker", worker.Dockerfile)
	}
}

func TestValidateLoopZeroYAML_LegacyNoV2Fields(t *testing.T) {
	content := `
project: legacy
apps:
  api:
    type: server
    port: 3000
    path: .
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("legacy yaml should be valid: %v", err)
	}
	if cfg.Version != 0 {
		t.Errorf("Version = %d, want 0 (unset)", cfg.Version)
	}
	if cfg.Apps["api"].Build != "" {
		t.Errorf("Build = %q, want empty (unset)", cfg.Apps["api"].Build)
	}
}
