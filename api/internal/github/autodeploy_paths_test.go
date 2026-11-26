// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
)

func newTestDeployer(t *testing.T, loopruntimeYaml string, cfg AutoDeployConfig) *AutoDeployer {
	t.Helper()
	files := map[string]string{"owner-repo-abc123/main.go": "package main"}
	if loopruntimeYaml != "" {
		files["owner-repo-abc123/loopzero.yaml"] = loopruntimeYaml
	}
	server := newGitHubTarballServerWithFiles(t, files)
	t.Cleanup(server.Close)
	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)
	cfg.AppClient = appClient
	cfg.Logger = slog.Default()
	return NewAutoDeployer(cfg)
}

const minimalLoopRuntimeYaml = "project: my-app\napps:\n  api:\n    runtime: node\n    start: node server.js\n"

func TestHandlePush_WithoutABuildSubmitterStillUploadsTheBundle(t *testing.T) {
	uploader := &fakeObjectUploader{}
	d := newTestDeployer(t, minimalLoopRuntimeYaml, AutoDeployConfig{ObjectStore: uploader})

	if err := d.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha1", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app",
	}); err != nil {
		t.Fatalf("HandlePush: %v", err)
	}
	if uploader.uploadedKey != "tenant-1/source/sha1.bundle" {
		t.Errorf("uploaded key = %q", uploader.uploadedKey)
	}
}

func TestPrepareSourceBundle_UploadsAndReturnsLoopRuntimeYaml(t *testing.T) {
	uploader := &fakeObjectUploader{}
	d := newTestDeployer(t, minimalLoopRuntimeYaml, AutoDeployConfig{ObjectStore: uploader, SourceBucket: "b"})

	y, err := d.PrepareSourceBundle(context.Background(), "owner/repo", "sha2", 1, "tenant-1")

	if err != nil {
		t.Fatalf("PrepareSourceBundle: %v", err)
	}
	if y == nil || y.Project != "my-app" {
		t.Errorf("loopzero.yaml = %+v", y)
	}
	if uploader.uploadedBucket != "b" || uploader.uploadedKey != "tenant-1/source/sha2.bundle" {
		t.Errorf("uploaded to %s/%s", uploader.uploadedBucket, uploader.uploadedKey)
	}
}

func TestPrepareSourceBundle_WithoutLoopRuntimeYamlStillUploads(t *testing.T) {
	uploader := &fakeObjectUploader{}
	d := newTestDeployer(t, "", AutoDeployConfig{ObjectStore: uploader})

	y, err := d.PrepareSourceBundle(context.Background(), "owner/repo", "sha3", 1, "tenant-1")

	if err != nil || y != nil {
		t.Fatalf("PrepareSourceBundle = (%+v, %v), want (nil, nil)", y, err)
	}
	if uploader.uploadedKey != "tenant-1/source/sha3.bundle" {
		t.Errorf("uploaded key = %q", uploader.uploadedKey)
	}
}

func TestPrepareSourceBundle_RejectsAnInvalidRepoName(t *testing.T) {
	d := newTestDeployer(t, minimalLoopRuntimeYaml, AutoDeployConfig{ObjectStore: &fakeObjectUploader{}})
	if _, err := d.PrepareSourceBundle(context.Background(), "no-slash", "sha", 1, "t"); err == nil {
		t.Fatal("expected an error for a repo name without owner")
	}
}

// Two conversions of the same commit — a push deploy and a PR preview, or two
// tenants deploying one upstream commit — must not share a bundle file: each
// one's cleanup would delete the other's bundle mid-upload.
func TestTarballToGitBundle_ConcurrentConversionsOfOneCommitDoNotCollide(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{"owner-repo-abc/main.go": "package main"}).Bytes()
	type result struct {
		path    string
		cleanup func()
		err     error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, c, err := tarballToGitBundle(context.Background(), bytes.NewReader(tarball), "abcdef0123456789", "msg", "u")
			results[i] = result{p, c, err}
		}(i)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			t.Fatalf("conversion failed: %v", r.err)
		}
	}
	if results[0].path == results[1].path {
		t.Fatalf("both conversions wrote %s", results[0].path)
	}
	results[0].cleanup()
	if _, err := os.Stat(results[1].path); err != nil {
		t.Errorf("cleaning up one conversion removed the other's bundle: %v", err)
	}
	results[1].cleanup()
}
