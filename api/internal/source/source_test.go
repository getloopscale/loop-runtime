// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// --- ValidateLoopZeroYAML additional coverage ---

func TestValidateLoopZeroYAML_InvalidYAML_ReturnsValidationError(t *testing.T) {
	content := `this: is: : : broken yaml`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "loopzero_yaml" {
		t.Errorf("Field = %q, want loopzero_yaml", ve.Field)
	}
}

func TestValidateLoopZeroYAML_MissingName_ReturnsValidationError(t *testing.T) {
	content := `
apps:
  api:
    type: server
    port: 3000
    path: .
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "name" {
		t.Errorf("Field = %q, want name", ve.Field)
	}
}

func TestValidateLoopZeroYAML_NoApps_ReturnsError(t *testing.T) {
	content := `
project: my-app
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error for no apps")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "apps" {
		t.Errorf("Field = %q, want apps", ve.Field)
	}
}

func TestValidateLoopZeroYAML_FrontendType_NoPortRequired(t *testing.T) {
	content := `
project: my-app
apps:
  web:
    type: frontend
    path: apps/web
    framework: react
    render: static
`
	cfg, names, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("frontend should not require port: %v", err)
	}
	if cfg.Apps["web"].Type != "frontend" {
		t.Errorf("Type = %q, want frontend", cfg.Apps["web"].Type)
	}
	if len(names) != 1 || names[0] != "web" {
		t.Errorf("names = %v, want [web]", names)
	}
}

func TestValidateLoopZeroYAML_DefaultTypeNoPort_RequiresPort(t *testing.T) {
	// When type is empty (default), port is required
	content := `
project: my-app
apps:
  api:
    path: .
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error: default type (empty) requires port")
	}
}

func TestValidateLoopZeroYAML_MultipleApps(t *testing.T) {
	content := `
project: monorepo
apps:
  api:
    type: server
    port: 3000
    path: apps/api
    framework: express
  worker:
    type: worker
    path: apps/worker
    framework: node
  web:
    type: frontend
    path: apps/web
    framework: react
`
	cfg, names, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 3 {
		t.Errorf("expected 3 apps, got %d", len(names))
	}
	if cfg.Name != "monorepo" {
		t.Errorf("Name = %q, want monorepo", cfg.Name)
	}
}

func TestValidateLoopZeroYAML_V2_Secrets(t *testing.T) {
	content := `
version: 2
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    secrets:
      - DATABASE_URL
      - REDIS_URL
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secrets := cfg.Apps["api"].Secrets
	if len(secrets) != 2 {
		t.Errorf("expected 2 secrets, got %d", len(secrets))
	}
}

func TestValidateLoopZeroYAML_NameFromProjectField(t *testing.T) {
	content := `
project: from-project-field
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
	if cfg.Name != "from-project-field" {
		t.Errorf("Name = %q, want from-project-field", cfg.Name)
	}
}

func TestValidateLoopZeroYAML_V2_CustomBuildStrategy(t *testing.T) {
	content := `
version: 1
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    build: custom
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Apps["api"].Build != "custom" {
		t.Errorf("Build = %q, want custom", cfg.Apps["api"].Build)
	}
}

// --- ValidationError ---

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{Field: "apps.api.port", Message: "must be > 0"}
	got := ve.Error()
	if got != "apps.api.port: must be > 0" {
		t.Errorf("Error() = %q, want 'apps.api.port: must be > 0'", got)
	}
}

// --- sourceRepoName ---

func TestSourceRepoName(t *testing.T) {
	got := sourceRepoName("tn-acme", "my-app")
	if got != "loopzero-source-tn-acme-my-app" {
		t.Errorf("sourceRepoName = %q, want loopzero-source-tn-acme-my-app", got)
	}

	got2 := sourceRepoName("tn-acme", "")
	if got2 != "loopzero-source-tn-acme" {
		t.Errorf("sourceRepoName (no project) = %q, want loopzero-source-tn-acme", got2)
	}
}

// --- SafeExtractTarGz error re-exports ---

func TestSafeExtractTarGz_Errors_AreExported(t *testing.T) {
	if ErrPathTraversal == nil {
		t.Error("ErrPathTraversal should not be nil")
	}
	if ErrSymlinkInArchive == nil {
		t.Error("ErrSymlinkInArchive should not be nil")
	}
	if ErrDecompressionLimit == nil {
		t.Error("ErrDecompressionLimit should not be nil")
	}
	if ErrFileCountLimit == nil {
		t.Error("ErrFileCountLimit should not be nil")
	}
}

// --- GiteaSourceStore constructor ---

func TestNewGiteaSourceStore_EmptyURL_ReturnsError(t *testing.T) {
	_, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: "",
		Token:   "token",
		OrgName: "loopzero",
	})
	if err == nil {
		t.Error("expected error for empty base URL")
	}
}

func TestNewGiteaSourceStore_ValidConfig(t *testing.T) {
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: "http://gitea:3000",
		Token:   "token",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Error("expected non-nil store")
	}
}

func TestComputeDirHash_DifferentContent_DifferentHash(t *testing.T) {
	dir1 := t.TempDir()
	mustWriteFile(t, filepath.Join(dir1, "a.txt"), []byte("hello"))

	dir2 := t.TempDir()
	mustWriteFile(t, filepath.Join(dir2, "a.txt"), []byte("world"))

	h1, err := computeDirHash(dir1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := computeDirHash(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("different content should produce different hashes")
	}
}

func TestComputeDirHash_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	h, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h) != 64 {
		t.Errorf("expected 64-char hex, got %d", len(h))
	}
}

// --- GiteaSourceStore.PushBundle empty bundle ---

func TestGiteaSourceStore_PushBundle_EmptyBundle(t *testing.T) {
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: "http://localhost:3000",
		Token:   "fake",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = store.PushBundle(context.Background(), "tenant", "project", bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected error for empty bundle")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty error, got: %v", err)
	}
}

// --- LoopZeroYAML struct fields ---

func TestLoopZeroYAML_Render(t *testing.T) {
	content := `
version: 2
project: my-app
apps:
  web:
    type: frontend
    path: apps/web
    framework: react
    render: static
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Apps["web"].Render != "static" {
		t.Errorf("Render = %q, want static", cfg.Apps["web"].Render)
	}
}

func TestLoopZeroYAML_Dockerfile(t *testing.T) {
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
	if cfg.Apps["api"].Dockerfile != "Dockerfile.prod" {
		t.Errorf("Dockerfile = %q, want Dockerfile.prod", cfg.Apps["api"].Dockerfile)
	}
}

// --- SourceStore interface ---

func TestSourceStore_InterfaceCompliance(t *testing.T) {
	// Compile-time check that GiteaSourceStore implements SourceStore
	var _ SourceStore = (*GiteaSourceStore)(nil)
}

// --- PushResult ---

func TestPushResult_Fields(t *testing.T) {
	r := PushResult{
		SourceID:      "abc123",
		GitBundlePath: "/tmp/bundle.git",
	}
	if r.SourceID != "abc123" {
		t.Errorf("SourceID = %q, want abc123", r.SourceID)
	}
	if r.GitBundlePath != "/tmp/bundle.git" {
		t.Errorf("GitBundlePath = %q, want /tmp/bundle.git", r.GitBundlePath)
	}
}
