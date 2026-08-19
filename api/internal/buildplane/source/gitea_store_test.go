// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run against real Gitea with real git bundles.
// Set GITEA_TEST_URL and GITEA_TEST_TOKEN to enable.

func skipIfNoGitea(t *testing.T) (string, string) {
	t.Helper()
	url := os.Getenv("GITEA_TEST_URL")
	if url == "" {
		t.Skip("GITEA_TEST_URL not set — skipping Gitea integration test")
	}
	token := os.Getenv("GITEA_TEST_TOKEN")
	if token == "" {
		t.Skip("GITEA_TEST_TOKEN not set — skipping Gitea integration test")
	}
	return url, token
}

func newTestGiteaStore(t *testing.T) *GiteaSourceStore {
	t.Helper()
	url, token := skipIfNoGitea(t)
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: url,
		Token:   token,
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return store
}

// createTestBundle creates a real git bundle from a temp repo with the given files.
func createTestBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "loopzero-test-repo-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Init git repo
	run(t, tmpDir, "git", "init")
	run(t, tmpDir, "git", "config", "user.email", "test@test.com")
	run(t, tmpDir, "git", "config", "user.name", "Test")

	// Write files
	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write file %s: %v", name, err)
		}
	}

	// Commit
	run(t, tmpDir, "git", "add", "-A")
	run(t, tmpDir, "git", "commit", "-m", "test commit")

	// Create bundle
	bundlePath := filepath.Join(tmpDir, "test.bundle")
	run(t, tmpDir, "git", "bundle", "create", bundlePath, "-1", "HEAD")

	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	return data
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, out)
	}
}

func TestGiteaSourceStore_PushRealBundle(t *testing.T) {
	store := newTestGiteaStore(t)
	ctx := context.Background()

	bundle := createTestBundle(t, map[string]string{
		"index.js":     `console.log("hello");`,
		"package.json": `{"name":"test","version":"1.0.0"}`,
		"loopzero.yaml":    "project: test\napps:\n  api:\n    port: 3000\n",
	})

	result, err := store.PushBundle(ctx, "acme", "push-test", bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("PushBundle failed: %v", err)
	}
	if result.SourceID == "" {
		t.Fatal("expected non-empty commit SHA")
	}
	if len(result.SourceID) != 40 {
		t.Errorf("expected 40-char SHA, got %d: %s", len(result.SourceID), result.SourceID)
	}
	t.Logf("pushed commit: %s", result.SourceID)
}

func TestGiteaSourceStore_PushEmptyBundle(t *testing.T) {
	store := newTestGiteaStore(t)
	ctx := context.Background()

	_, err := store.PushBundle(ctx, "acme", "empty-test", bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected error for empty bundle, got nil")
	}
}

func TestGiteaSourceStore_PushTwiceDifferentContent(t *testing.T) {
	store := newTestGiteaStore(t)
	ctx := context.Background()

	bundle1 := createTestBundle(t, map[string]string{
		"index.js": `console.log("v1");`,
	})
	r1, err := store.PushBundle(ctx, "acme", "update-test", bytes.NewReader(bundle1))
	if err != nil {
		t.Fatalf("first push failed: %v", err)
	}

	bundle2 := createTestBundle(t, map[string]string{
		"index.js": `console.log("v2");`,
	})
	r2, err := store.PushBundle(ctx, "acme", "update-test", bytes.NewReader(bundle2))
	if err != nil {
		t.Fatalf("second push failed: %v", err)
	}

	if r1.SourceID == r2.SourceID {
		t.Error("expected different SHAs for different content")
	}
	t.Logf("v1: %s, v2: %s", r1.SourceID, r2.SourceID)
}

func TestGiteaSourceStore_ClonableAfterPush(t *testing.T) {
	store := newTestGiteaStore(t)
	ctx := context.Background()
	url, _ := skipIfNoGitea(t)

	bundle := createTestBundle(t, map[string]string{
		"app.js":       `const x = 1;`,
		"package.json": `{"name":"clone-test"}`,
	})

	if _, err := store.PushBundle(ctx, "acme", "clone-test", bytes.NewReader(bundle)); err != nil {
		t.Fatalf("PushBundle failed: %v", err)
	}

	// Now verify the repo is cloneable from Gitea (with auth — private repo)
	cloneDir, err := os.MkdirTemp("", "loopzero-clone-verify-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(cloneDir) }()

	_, token := skipIfNoGitea(t)
	cloneURL := strings.Replace(url, "://", fmt.Sprintf("://oauth2:%s@", token), 1) + "/loopzero/loopzero-source-acme-clone-test.git"
	run(t, cloneDir, "git", "clone", cloneURL, filepath.Join(cloneDir, "repo"))

	// Verify files exist
	content, err := os.ReadFile(filepath.Join(cloneDir, "repo", "app.js"))
	if err != nil {
		t.Fatalf("cloned repo missing app.js: %v", err)
	}
	if string(content) != "const x = 1;" {
		t.Errorf("expected 'const x = 1;', got %q", string(content))
	}
}
