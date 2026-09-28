// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Helpers to build in-memory tarballs for testing ---

// makeTarGz creates a gzipped tarball in memory with the given file entries.
// Each entry is a path→content pair.
func makeTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for path, content := range files {
		hdr := &tar.Header{
			Name: path,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header for %q: %v", path, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar content for %q: %v", path, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return &buf
}

// --- extractLoopZeroYaml tests ---

func TestExtractLoopZeroYaml_ValidTarball(t *testing.T) {
	// Arrange — GitHub tarball layout: "reponame-sha/loopzero.yaml"
	yamlContent := `project: my-app
apps:
  api:
    language: go
`
	tarball := makeTarGz(t, map[string]string{
		"owner-repo-abc123/loopzero.yaml": yamlContent,
		"owner-repo-abc123/main.go":   "package main",
		"owner-repo-abc123/go.mod":    "module example",
	})

	// Act
	cfg, err := extractLoopZeroYaml(tarball)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Project != "my-app" {
		t.Errorf("expected project 'my-app', got %q", cfg.Project)
	}
	if len(cfg.Apps) != 1 {
		t.Errorf("expected 1 app, got %d", len(cfg.Apps))
	}
	if _, ok := cfg.Apps["api"]; !ok {
		t.Error("expected 'api' app in apps map")
	}
}

func TestExtractLoopZeroYaml_MissingFile(t *testing.T) {
	// Arrange — tarball without loopzero.yaml
	tarball := makeTarGz(t, map[string]string{
		"owner-repo-abc123/main.go": "package main",
		"owner-repo-abc123/go.mod":  "module example",
	})

	// Act
	cfg, err := extractLoopZeroYaml(tarball)

	// Assert
	if err == nil {
		t.Fatal("expected error when loopzero.yaml is missing, got nil")
	}
	if cfg != nil {
		t.Fatal("expected nil config when loopzero.yaml is missing")
	}
	if !strings.Contains(err.Error(), "loopzero.yaml not found") {
		t.Errorf("expected 'loopzero.yaml not found' error, got: %v", err)
	}
}

func TestExtractLoopZeroYaml_MalformedYaml(t *testing.T) {
	// Arrange — loopzero.yaml with invalid YAML
	tarball := makeTarGz(t, map[string]string{
		"owner-repo-abc123/loopzero.yaml": "{{invalid: yaml: [",
	})

	// Act
	cfg, err := extractLoopZeroYaml(tarball)

	// Assert
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
	if cfg != nil {
		t.Fatal("expected nil config for malformed YAML")
	}
	var invalid *InvalidDeployDescriptorError
	if !errors.As(err, &invalid) {
		t.Errorf("expected the deploy descriptor refusal, got: %v", err)
	}
}

func TestExtractLoopZeroYaml_NestedPath(t *testing.T) {
	// Arrange — GitHub tarball: "reponame-sha/loopzero.yaml" at depth=2 (prefix/file)
	tarball := makeTarGz(t, map[string]string{
		"getloopscale-loopzerokit-example-abc123/loopzero.yaml": `project: nested-project
apps:
  web:
    language: node
`,
	})

	// Act
	cfg, err := extractLoopZeroYaml(tarball)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Project != "nested-project" {
		t.Errorf("expected project 'nested-project', got %q", cfg.Project)
	}
}

func TestExtractLoopZeroYaml_DeeplyNestedIgnored(t *testing.T) {
	// Arrange — loopzero.yaml at depth 3+ should NOT be found (only depth=2 is repo root)
	tarball := makeTarGz(t, map[string]string{
		"owner-repo-abc/subdir/loopzero.yaml": `project: deep`,
	})

	// Act
	cfg, err := extractLoopZeroYaml(tarball)

	// Assert
	if err == nil {
		t.Fatal("expected error for deeply nested loopzero.yaml, got nil")
	}
	if cfg != nil {
		t.Fatal("expected nil config")
	}
}

func TestExtractLoopZeroYaml_InvalidGzip(t *testing.T) {
	// Arrange — not a valid gzip stream
	buf := bytes.NewBufferString("this is not gzip data")

	// Act
	cfg, err := extractLoopZeroYaml(buf)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid gzip, got nil")
	}
	if cfg != nil {
		t.Fatal("expected nil config")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("expected gzip error, got: %v", err)
	}
}

func TestExtractLoopZeroYaml_EmptyProject(t *testing.T) {
	// Arrange — valid YAML but empty project name
	tarball := makeTarGz(t, map[string]string{
		"prefix/loopzero.yaml": `apps:
  api:
    language: go
`,
	})

	// Act
	cfg, err := extractLoopZeroYaml(tarball)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Project != "" {
		t.Errorf("expected empty project, got %q", cfg.Project)
	}
	if len(cfg.Apps) != 1 {
		t.Errorf("expected 1 app, got %d", len(cfg.Apps))
	}
}

func TestExtractLoopZeroYaml_MultipleApps(t *testing.T) {
	// Arrange
	tarball := makeTarGz(t, map[string]string{
		"prefix/loopzero.yaml": `project: multi
apps:
  api:
    language: go
  web:
    language: node
  worker:
    language: python
`,
	})

	// Act
	cfg, err := extractLoopZeroYaml(tarball)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Project != "multi" {
		t.Errorf("expected project 'multi', got %q", cfg.Project)
	}
	if len(cfg.Apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(cfg.Apps))
	}
}

// --- Fakes for HandlePush dependencies ---

type fakeObjectUploader struct {
	uploadedBucket string
	uploadedKey    string
	err            error
}

func (f *fakeObjectUploader) Upload(ctx context.Context, bucket, key string, data io.Reader) error {
	f.uploadedBucket = bucket
	f.uploadedKey = key
	return f.err
}

type fakeBuildSubmitter struct {
	submittedReq *WebhookBuildRequest
	err          error
}

func (f *fakeBuildSubmitter) SubmitBuildFromWebhook(ctx context.Context, req WebhookBuildRequest) error {
	f.submittedReq = &req
	return f.err
}

type fakeBuildCanceller struct {
	cancelled int
	err       error
}

func (f *fakeBuildCanceller) CancelQueuedBuilds(ctx context.Context, tenantID, projectID, environment string) (int, error) {
	return f.cancelled, f.err
}

// newGitHubTarballServer creates a test HTTP server that serves a tarball with loopzero.yaml
// at the GitHub API tarball download endpoint, and handles installation token requests.
func newGitHubTarballServer(t *testing.T, loopzeroYamlContent string) *httptest.Server {
	t.Helper()
	return newGitHubTarballServerWithFiles(t, map[string]string{
		"owner-repo-abc123/loopzero.yaml": loopzeroYamlContent,
		"owner-repo-abc123/main.go":   "package main",
	})
}

// newGitHubTarballServerWithFiles serves a tarball holding exactly files.
func newGitHubTarballServerWithFiles(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	tarballBytes := makeTarGz(t, files).Bytes()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tarball/") {
			w.Header().Set("Content-Type", "application/gzip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tarballBytes)
			return
		}
		if strings.Contains(r.URL.Path, "/statuses/") {
			w.WriteHeader(http.StatusCreated)
			return
		}
		// Default: installation token
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_test_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
}

func TestHandlePush_HappyPath(t *testing.T) {
	// Arrange
	server := newGitHubTarballServer(t, `project: my-app
apps:
  api:
    language: go
`)
	defer server.Close()

	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)

	uploader := &fakeObjectUploader{}
	submitter := &fakeBuildSubmitter{}
	canceller := &fakeBuildCanceller{cancelled: 0}

	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient:      appClient,
		ObjectStore:    uploader,
		BuildSubmitter: submitter,
		BuildCanceller: canceller,
		SourceBucket:   "test-bucket",
		Logger:         slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "abc123def456", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app", CommitMessage: "feat: add feature", SenderLogin: "testuser",
	})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify S3 upload happened
	if uploader.uploadedBucket != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %q", uploader.uploadedBucket)
	}
	expectedKey := "tenant-1/source/abc123def456.bundle"
	if uploader.uploadedKey != expectedKey {
		t.Errorf("expected key %q, got %q", expectedKey, uploader.uploadedKey)
	}

	// Verify build was submitted
	if submitter.submittedReq == nil {
		t.Fatal("expected build to be submitted")
	}
	if submitter.submittedReq.TenantID != "tenant-1" {
		t.Errorf("expected tenantID 'tenant-1', got %q", submitter.submittedReq.TenantID)
	}
	if submitter.submittedReq.ProjectName != "my-app" {
		t.Errorf("expected projectName 'my-app', got %q", submitter.submittedReq.ProjectName)
	}
	if submitter.submittedReq.CommitSHA != "abc123def456" {
		t.Errorf("expected commitSHA 'abc123def456', got %q", submitter.submittedReq.CommitSHA)
	}
	if submitter.submittedReq.Environment != "production" {
		t.Errorf("expected environment 'production', got %q", submitter.submittedReq.Environment)
	}
	if submitter.submittedReq.LoopZeroYaml == nil {
		t.Fatal("expected LoopZeroYaml to be set")
	}
	if submitter.submittedReq.LoopZeroYaml.Project != "my-app" {
		t.Errorf("expected LoopZeroYaml.Project 'my-app', got %q", submitter.submittedReq.LoopZeroYaml.Project)
	}
}

func TestHandlePush_DownloadFailure(t *testing.T) {
	// Arrange — server returns 500 on tarball download
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tarball/") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)

	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient: appClient,
		Logger:    slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "project-1", CommitMessage: "msg", SenderLogin: "user",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error when download fails, got nil")
	}
	if !strings.Contains(err.Error(), "download source") {
		t.Errorf("expected 'download source' in error, got: %v", err)
	}
}

func TestHandlePush_NoLoopZeroYaml_ReturnsNil(t *testing.T) {
	// Arrange — tarball WITHOUT loopzero.yaml
	tarball := makeTarGz(t, map[string]string{
		"owner-repo-abc123/main.go": "package main",
	})
	tarballBytes := tarball.Bytes()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tarball/") {
			w.Header().Set("Content-Type", "application/gzip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tarballBytes)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)

	submitter := &fakeBuildSubmitter{}
	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient:      appClient,
		BuildSubmitter: submitter,
		Logger:         slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "project-1", CommitMessage: "msg", SenderLogin: "user",
	})

	// Assert — no loopzero.yaml means not a LoopZero project, returns nil (not error)
	if err != nil {
		t.Fatalf("expected nil (not a LoopZero project), got %v", err)
	}
	// Build should NOT have been submitted
	if submitter.submittedReq != nil {
		t.Error("build should not be submitted when loopzero.yaml is missing")
	}
}

func TestHandlePush_BuildSubmitFailure(t *testing.T) {
	// Arrange
	server := newGitHubTarballServer(t, `project: my-app
apps:
  api:
    language: go
`)
	defer server.Close()

	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)

	submitter := &fakeBuildSubmitter{err: fmt.Errorf("orchestrator unavailable")}
	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient:      appClient,
		BuildSubmitter: submitter,
		SourceBucket:   "test-bucket",
		ObjectStore:    &fakeObjectUploader{},
		Logger:         slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app", CommitMessage: "msg", SenderLogin: "user",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error when build submit fails, got nil")
	}
	if !strings.Contains(err.Error(), "submit build") {
		t.Errorf("expected 'submit build' in error, got: %v", err)
	}
}

func TestHandlePush_CancelQueuedNonFatal(t *testing.T) {
	// Arrange — canceller returns error, but HandlePush should still succeed
	server := newGitHubTarballServer(t, `project: my-app
apps:
  api:
    language: go
`)
	defer server.Close()

	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)

	canceller := &fakeBuildCanceller{err: fmt.Errorf("db connection lost")}
	submitter := &fakeBuildSubmitter{}
	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient:      appClient,
		BuildCanceller: canceller,
		BuildSubmitter: submitter,
		ObjectStore:    &fakeObjectUploader{},
		SourceBucket:   "test-bucket",
		Logger:         slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app", CommitMessage: "msg", SenderLogin: "user",
	})

	// Assert — should succeed despite cancel error
	if err != nil {
		t.Fatalf("expected no error (cancel is non-fatal), got %v", err)
	}
	if submitter.submittedReq == nil {
		t.Error("expected build to be submitted despite cancel error")
	}
}

func TestHandlePush_InvalidRepoName(t *testing.T) {
	// Arrange — repo name without "/" separator
	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient: &AppClient{}, // won't be called
		Logger:    slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "invalid-repo-no-slash", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "project-1", CommitMessage: "msg", SenderLogin: "user",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid repo name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid repo name") {
		t.Errorf("expected 'invalid repo name' error, got: %v", err)
	}
}

func TestHandlePush_DefaultBucket(t *testing.T) {
	// Arrange — no SourceBucket set, should default to "loopzero-builds"
	server := newGitHubTarballServer(t, `project: my-app
apps:
  api:
    language: go
`)
	defer server.Close()

	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)

	uploader := &fakeObjectUploader{}
	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient:      appClient,
		ObjectStore:    uploader,
		BuildSubmitter: &fakeBuildSubmitter{},
		SourceBucket:   "", // empty — should default
		Logger:         slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app", CommitMessage: "msg", SenderLogin: "user",
	})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if uploader.uploadedBucket != "loopzero-builds" {
		t.Errorf("expected default bucket 'loopzero-builds', got %q", uploader.uploadedBucket)
	}
}

func TestHandlePush_S3UploadFailure(t *testing.T) {
	// Arrange
	server := newGitHubTarballServer(t, `project: my-app
apps:
  api:
    language: go
`)
	defer server.Close()

	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)

	uploader := &fakeObjectUploader{err: fmt.Errorf("S3 unreachable")}
	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient:    appClient,
		ObjectStore:  uploader,
		SourceBucket: "test-bucket",
		Logger:       slog.Default(),
	})

	// Act
	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app", CommitMessage: "msg", SenderLogin: "user",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error when S3 upload fails, got nil")
	}
	if !strings.Contains(err.Error(), "S3 upload") {
		t.Errorf("expected 'S3 upload' in error, got: %v", err)
	}
}
