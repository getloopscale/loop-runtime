// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitops

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// These tests run against real Gitea.
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

func newTestPusher(t *testing.T) *GiteaGitOpsPusher {
	t.Helper()
	url, token := skipIfNoGitea(t)
	return NewGiteaGitOpsPusher(GiteaGitOpsPusherConfig{
		BaseURL: url,
		Token:   token,
		OrgName: "loop-runtime",
	})
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const testManifests = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: acme-production
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: myapp
  namespace: acme-production
`

func TestGiteaGitOpsPusher_PushManifests(t *testing.T) {
	pusher := newTestPusher(t)
	ctx := context.Background()

	commitSHA, err := pusher.Push(ctx, deploycontracts.GitOpsPushOpts{
		TenantID:      "acme",
		ProjectID:     "myapp",
		Environment:   "production",
		Manifests:     testManifests,
		BuildID:       "bld-abc123",
		ReleaseID:     "rel-def456",
		CorrelationID: "corr-789",
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if commitSHA == "" {
		t.Fatal("expected non-empty commit SHA")
	}
	if len(commitSHA) != 40 {
		t.Errorf("expected 40-char SHA, got %d: %s", len(commitSHA), commitSHA)
	}
	t.Logf("pushed commit: %s", commitSHA)
}

func TestGiteaGitOpsPusher_PushCreatesCorrectPath(t *testing.T) {
	pusher := newTestPusher(t)
	ctx := context.Background()
	url, token := skipIfNoGitea(t)

	_, err := pusher.Push(ctx, deploycontracts.GitOpsPushOpts{
		TenantID:      "acme",
		ProjectID:     "pathtest",
		Environment:   "staging",
		Manifests:     testManifests,
		BuildID:       "bld-path1",
		ReleaseID:     "rel-path1",
		CorrelationID: "corr-path1",
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Clone and verify file path: staging/pathtest/manifests.yaml (BLOCKER-3: env-first hierarchy)
	cloneDir, err := os.MkdirTemp("", "loop-runtime-gitops-verify-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(cloneDir) }()

	cloneURL := strings.Replace(url, "://", fmt.Sprintf("://oauth2:%s@", token), 1) + "/loop-runtime/acme-gitops.git"
	gitRun(t, cloneDir, "git", "clone", cloneURL, filepath.Join(cloneDir, "repo"))

	content, err := os.ReadFile(filepath.Join(cloneDir, "repo", "staging", "pathtest", "manifests.yaml"))
	if err != nil {
		t.Fatalf("expected staging/pathtest/manifests.yaml in gitops repo: %v", err)
	}
	if string(content) != testManifests {
		t.Errorf("manifest content mismatch:\ngot:  %q\nwant: %q", string(content), testManifests)
	}
}

func TestGiteaGitOpsPusher_PushHasGitTrailers(t *testing.T) {
	pusher := newTestPusher(t)
	ctx := context.Background()
	url, token := skipIfNoGitea(t)

	_, err := pusher.Push(ctx, deploycontracts.GitOpsPushOpts{
		TenantID:      "acme",
		ProjectID:     "trailertest",
		Environment:   "production",
		Manifests:     testManifests,
		BuildID:       "bld-t1",
		ReleaseID:     "rel-t1",
		CorrelationID: "corr-t1",
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Clone and check commit message trailers
	cloneDir, err := os.MkdirTemp("", "loop-runtime-gitops-trailers-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(cloneDir) }()

	cloneURL := strings.Replace(url, "://", fmt.Sprintf("://oauth2:%s@", token), 1) + "/loop-runtime/acme-gitops.git"
	gitRun(t, cloneDir, "git", "clone", cloneURL, filepath.Join(cloneDir, "repo"))

	logOutput := gitRun(t, filepath.Join(cloneDir, "repo"), "git", "log", "-1", "--format=%B")

	for _, trailer := range []string{"Release-ID: rel-t1", "Correlation-ID: corr-t1", "Build-ID: bld-t1"} {
		if !strings.Contains(logOutput, trailer) {
			t.Errorf("commit message missing trailer %q\ngot: %s", trailer, logOutput)
		}
	}
}

func TestGiteaGitOpsPusher_PushIdempotent_SameContent(t *testing.T) {
	pusher := newTestPusher(t)
	ctx := context.Background()

	opts := deploycontracts.GitOpsPushOpts{
		TenantID:      "acme",
		ProjectID:     "idempotent",
		Environment:   "production",
		Manifests:     testManifests,
		BuildID:       "bld-idem1",
		ReleaseID:     "rel-idem1",
		CorrelationID: "corr-idem1",
	}

	sha1, err := pusher.Push(ctx, opts)
	if err != nil {
		t.Fatalf("first push failed: %v", err)
	}

	// Second push with same content — should succeed (idempotent, no error)
	sha2, err := pusher.Push(ctx, opts)
	if err != nil {
		t.Fatalf("second push (idempotent) failed: %v", err)
	}

	// Different SHAs expected because trailers include release-id (same here)
	// but the commit message is identical, so git may produce the same tree
	// Either way, both should succeed without error
	t.Logf("sha1=%s sha2=%s", sha1, sha2)
}

func TestGiteaGitOpsPusher_EmptyManifests(t *testing.T) {
	// This test validates input checking — no Gitea needed.
	pusher := NewGiteaGitOpsPusher(GiteaGitOpsPusherConfig{
		BaseURL: "http://localhost:3000",
		Token:   "fake",
		OrgName: "loop-runtime",
	})
	ctx := context.Background()

	_, err := pusher.Push(ctx, deploycontracts.GitOpsPushOpts{
		TenantID:    "acme",
		ProjectID:   "empty",
		Environment: "production",
		Manifests:   "",
		BuildID:     "bld-empty",
		ReleaseID:   "rel-empty",
	})
	if err == nil {
		t.Fatal("expected error for empty manifests")
	}
	if !strings.Contains(err.Error(), "manifests cannot be empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGiteaGitOpsPusher_GetManifestsAtCommit_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	pusher := &GiteaGitOpsPusher{
		baseURL: srv.URL,
		token:   "test-token",
		orgName: "loop-runtime",
		client:  srv.Client(),
	}

	_, err := pusher.GetManifestsAtCommit(context.Background(), "acme", "myapp", "production", "abc123")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestGiteaGitOpsPusher_GetManifestsAtCommit_Success(t *testing.T) {
	manifest := "apiVersion: apps/v1\nkind: Deployment\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(manifest))
	}))
	defer srv.Close()

	pusher := &GiteaGitOpsPusher{
		baseURL: srv.URL,
		token:   "test-token",
		orgName: "loop-runtime",
		client:  srv.Client(),
	}

	data, err := pusher.GetManifestsAtCommit(context.Background(), "acme", "myapp", "production", "abc123")
	if err != nil {
		t.Fatalf("GetManifestsAtCommit failed: %v", err)
	}
	if string(data) != manifest {
		t.Errorf("got %q, want %q", string(data), manifest)
	}
}

func TestGiteaGitOpsPusher_GetManifestsAtCommit_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	pusher := &GiteaGitOpsPusher{
		baseURL: srv.URL,
		token:   "test-token",
		orgName: "loop-runtime",
		client:  srv.Client(),
	}

	_, err := pusher.GetManifestsAtCommit(context.Background(), "acme", "myapp", "production", "abc123")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestGiteaGitOpsPusher_GetManifestsAtCommit_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pusher := &GiteaGitOpsPusher{
		baseURL: srv.URL,
		token:   "test-token",
		orgName: "loop-runtime",
		client:  srv.Client(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pusher.GetManifestsAtCommit(ctx, "acme", "myapp", "prod", "abc")
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

func TestGiteaGitOpsPusher_Push_ForbiddenManifest(t *testing.T) {
	// Push should fail if manifest contains a forbidden kind.
	// This test validates the manifest gate integration in Push().
	pusher := NewGiteaGitOpsPusher(GiteaGitOpsPusherConfig{
		BaseURL: "http://localhost:3000",
		Token:   "fake",
		OrgName: "loop-runtime",
	})

	forbiddenManifest := "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: evil\n"
	_, err := pusher.Push(context.Background(), deploycontracts.GitOpsPushOpts{
		TenantID:      "acme",
		ProjectID:     "app",
		Environment:   "production",
		Manifests:     forbiddenManifest,
		BuildID:       "bld-1",
		ReleaseID:     "rel-1",
		CorrelationID: "corr-1",
	})
	if err == nil {
		t.Fatal("expected error for forbidden manifest kind")
	}
	if !strings.Contains(err.Error(), "manifest validation failed") {
		t.Errorf("expected manifest validation error, got: %v", err)
	}
}

func TestGiteaGitOpsPusher_UpdateExistingManifests(t *testing.T) {
	pusher := newTestPusher(t)
	ctx := context.Background()

	// Push v1
	sha1, err := pusher.Push(ctx, deploycontracts.GitOpsPushOpts{
		TenantID:      "acme",
		ProjectID:     "updatetest",
		Environment:   "production",
		Manifests:     "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: v1\n",
		BuildID:       "bld-u1",
		ReleaseID:     "rel-u1",
		CorrelationID: "corr-u1",
	})
	if err != nil {
		t.Fatalf("v1 push failed: %v", err)
	}

	// Push v2 (updated manifests)
	sha2, err := pusher.Push(ctx, deploycontracts.GitOpsPushOpts{
		TenantID:      "acme",
		ProjectID:     "updatetest",
		Environment:   "production",
		Manifests:     "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: v2\n",
		BuildID:       "bld-u2",
		ReleaseID:     "rel-u2",
		CorrelationID: "corr-u2",
	})
	if err != nil {
		t.Fatalf("v2 push failed: %v", err)
	}

	if sha1 == sha2 {
		t.Error("expected different SHAs for different manifests")
	}
}
