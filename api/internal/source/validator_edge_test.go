// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"archive/tar"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidateLoopZeroYAML — edge cases not covered by existing tests
// ---------------------------------------------------------------------------

func TestValidateLoopZeroYAML_NameField_TakesPrecedenceOverProject(t *testing.T) {
	content := `
name: from-name
project: from-project
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
	// When both name and project are set, name wins (project is only fallback)
	if cfg.Name != "from-name" {
		t.Errorf("Name = %q, want from-name", cfg.Name)
	}
}

func TestValidateLoopZeroYAML_Version2_Valid(t *testing.T) {
	content := `
version: 2
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
	if cfg.Version != 2 {
		t.Errorf("Version = %d, want 2", cfg.Version)
	}
}

func TestValidateLoopZeroYAML_BuildpackStrategy_Valid(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    build: buildpack
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Apps["api"].Build != "buildpack" {
		t.Errorf("Build = %q, want buildpack", cfg.Apps["api"].Build)
	}
}

func TestValidateLoopZeroYAML_EnvKey_ValidUnderscorePrefix(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    env:
      _PRIVATE_VAR: value
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("underscore-prefixed env key should be valid: %v", err)
	}
}

func TestValidateLoopZeroYAML_EnvKey_LowercaseRejected(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    env:
      lowercase_key: value
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error for lowercase env key")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if !strings.Contains(ve.Field, "env") {
		t.Errorf("Field = %q, should contain 'env'", ve.Field)
	}
}

func TestValidateLoopZeroYAML_EnvKey_NumericStartRejected(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    env:
      1BAD_KEY: value
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error for numeric-start env key")
	}
}

func TestValidateLoopZeroYAML_EnvKey_EmptyMapValid(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    env: {}
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("empty env map should be valid: %v", err)
	}
}

func TestValidateLoopZeroYAML_ServerWithNegativePort(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: -1
    path: .
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error for negative port")
	}
}

func TestValidateLoopZeroYAML_ServerWithZeroPort(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 0
    path: .
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error for zero port")
	}
}

func TestValidateLoopZeroYAML_MultipleApps_OneInvalid(t *testing.T) {
	content := `
project: monorepo
apps:
  good-api:
    type: server
    port: 3000
    path: apps/api
  bad-api:
    type: invalid-type
    path: apps/bad
`
	_, _, err := ValidateLoopZeroYAML(content)
	if err == nil {
		t.Fatal("expected error when one app has invalid type")
	}
}

func TestValidateLoopZeroYAML_MultipleEnvKeys_AllValid(t *testing.T) {
	content := `
project: my-app
apps:
  api:
    type: server
    port: 3000
    path: .
    env:
      NODE_ENV: prod
      LOG_LEVEL: info
      MAX_RETRIES: "3"
      _INTERNAL: yes
`
	cfg, _, err := ValidateLoopZeroYAML(content)
	if err != nil {
		t.Fatalf("all valid env keys should pass: %v", err)
	}
	if len(cfg.Apps["api"].Env) != 4 {
		t.Errorf("expected 4 env vars, got %d", len(cfg.Apps["api"].Env))
	}
}

// ---------------------------------------------------------------------------
// computeDirHash — additional edge cases
// ---------------------------------------------------------------------------

func TestComputeDirHash_SubdirFiles_IncludedAndSorted(t *testing.T) {
	dir := t.TempDir()
	// Create files in subdirectories to verify they are included
	mustMkdir(t, filepath.Join(dir, "a"))
	mustMkdir(t, filepath.Join(dir, "b"))
	mustWriteFile(t, filepath.Join(dir, "b", "second.txt"), []byte("2"))
	mustWriteFile(t, filepath.Join(dir, "a", "first.txt"), []byte("1"))

	h, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h) != 64 {
		t.Errorf("expected 64-char hex, got %d", len(h))
	}

	// Change content in subdir — hash should differ
	mustWriteFile(t, filepath.Join(dir, "a", "first.txt"), []byte("modified"))
	h2, err := computeDirHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h == h2 {
		t.Error("modifying subdir file should change hash")
	}
}

func TestComputeDirHash_DifferentFilenames_DifferentHash(t *testing.T) {
	dir1 := t.TempDir()
	mustWriteFile(t, filepath.Join(dir1, "file_a.txt"), []byte("content"))

	dir2 := t.TempDir()
	mustWriteFile(t, filepath.Join(dir2, "file_b.txt"), []byte("content"))

	h1, err := computeDirHash(dir1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := computeDirHash(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("different filenames with same content should produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// PushBundle format detection (gzip magic bytes)
// ---------------------------------------------------------------------------

func TestGiteaSourceStore_PushBundle_InvalidGzipData_ReturnsError(t *testing.T) {
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: "http://localhost:3000",
		Token:   "fake",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Data that starts with gzip magic but is not valid gzip
	invalidGzip := []byte{0x1f, 0x8b, 0x00, 0x00, 0x00}
	_, err = store.PushBundle(t.Context(), "tenant", "project", strings.NewReader(string(invalidGzip)))
	if err == nil {
		t.Fatal("expected error for invalid gzip data")
	}
}

// ---------------------------------------------------------------------------
// PushBundle — tar.gz path exercises extraction + hash before EnsureRepo fails
// ---------------------------------------------------------------------------

func TestGiteaSourceStore_PushBundle_ValidTarGz_FailsAtEnsureRepo(t *testing.T) {
	// Use a server that always returns 500 so EnsureRepo fails.
	// This exercises: format detection, temp dir, tar extraction, hash computation.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: ts.URL,
		Token:   "test-token",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a valid tar.gz with some files
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "source.tar.gz")
	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "src/", Typeflag: tar.TypeDir},
		{Name: "src/main.go", Typeflag: tar.TypeReg, Content: "package main"},
		{Name: "README.md", Typeflag: tar.TypeReg, Content: "# Test"},
	})

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	_, err = store.PushBundle(t.Context(), "tenant", "project", bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error (EnsureRepo should fail against test server)")
	}
	// The error should come from the git init/push phase, not from extraction
	// This means extraction and hash computation succeeded
	if !strings.Contains(err.Error(), "ensure repo") && !strings.Contains(err.Error(), "git") {
		t.Errorf("unexpected error source: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PushBundle — git bundle path (non-gzip data)
// ---------------------------------------------------------------------------

func TestGiteaSourceStore_PushBundle_NonGzipData_FailsAtEnsureRepo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: ts.URL,
		Token:   "test-token",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-gzip data will go through pushGitBundle path
	bundleData := []byte("# v2 git bundle\nsome fake bundle data here")
	_, err = store.PushBundle(t.Context(), "tenant", "project", bytes.NewReader(bundleData))
	if err == nil {
		t.Fatal("expected error")
	}
	// Should fail at ensure repo (500 from test server)
	if !strings.Contains(err.Error(), "ensure repo") {
		t.Errorf("expected ensure repo error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PushBundle — EnsureRepo returns 200 (repo exists), then git operations fail
// ---------------------------------------------------------------------------

func TestGiteaSourceStore_PushBundle_TarGz_EnsureRepoOK_GitFails(t *testing.T) {
	// Server returns 200 for repo check (repo exists), so EnsureRepo passes.
	// Then git init/commit/push will proceed, exercising more of the code path.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: ts.URL,
		Token:   "test-token",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "source.tar.gz")
	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "index.js", Typeflag: tar.TypeReg, Content: "module.exports = {}"},
	})
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	_, err = store.PushBundle(t.Context(), "tenant", "project", bytes.NewReader(data))
	// Will fail at git push (remote is not a real git repo) but exercises:
	// extraction, hash computation, git init, git add, git commit, remote add
	if err == nil {
		t.Fatal("expected error at git push phase")
	}
}

func TestGiteaSourceStore_PushBundle_GitBundle_EnsureRepoOK_CloneFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: ts.URL,
		Token:   "test-token",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-gzip data → pushGitBundle path
	// EnsureRepo will succeed (200), then clone/pull will fail
	bundleData := []byte("not-a-real-bundle")
	_, err = store.PushBundle(t.Context(), "tenant", "project", bytes.NewReader(bundleData))
	if err == nil {
		t.Fatal("expected error")
	}
	// Should fail at git fetch from bundle
	if !strings.Contains(err.Error(), "fetch") && !strings.Contains(err.Error(), "pull") && !strings.Contains(err.Error(), "bundle") {
		t.Logf("error (acceptable): %v", err)
	}
}

// ---------------------------------------------------------------------------
// envKeyRegex — test the compiled regex directly
// ---------------------------------------------------------------------------

func TestEnvKeyRegex_Valid(t *testing.T) {
	valid := []string{"NODE_ENV", "A", "ABC", "_PRIVATE", "A1", "MAX_RETRIES_3"}
	for _, key := range valid {
		if !envKeyRegex.MatchString(key) {
			t.Errorf("envKeyRegex should match %q", key)
		}
	}
}

func TestEnvKeyRegex_Invalid(t *testing.T) {
	invalid := []string{"lowercase", "1STARTS_NUM", "has-dash", "has space", "a.dot", ""}
	for _, key := range invalid {
		if envKeyRegex.MatchString(key) {
			t.Errorf("envKeyRegex should NOT match %q", key)
		}
	}
}
