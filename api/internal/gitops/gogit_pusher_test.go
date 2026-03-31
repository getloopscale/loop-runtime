// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/loopzero/loopzero/api/internal/services"
)

// initBareRepo creates a bare git repo in a temp dir and returns the path.
// The bare repo acts as the "remote" for tests (no Gitea needed).
func initBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	return dir
}

// newLocalPusher creates a GoGitGitOpsPusher that points at a local bare repo.
func newLocalPusher(t *testing.T, bareDir string) *GoGitGitOpsPusher {
	t.Helper()
	return NewGoGitGitOpsPusher(GiteaGitOpsPusherConfig{
		BaseURL: "file://" + bareDir,
		Token:   "test-token",
		OrgName: "loopzero",
	})
}

// cloneBareAndReadFile clones the bare repo into a temp dir and reads a file.
func cloneBareAndReadFile(t *testing.T, bareDir, filePath string) string {
	t.Helper()
	cloneDir := t.TempDir()
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL:           bareDir,
		ReferenceName: plumbing.Main,
		SingleBranch:  true,
	})
	if err != nil {
		t.Fatalf("clone bare repo: %v", err)
	}
	_ = repo

	data, err := os.ReadFile(filepath.Join(cloneDir, filePath))
	if err != nil {
		t.Fatalf("read file %s: %v", filePath, err)
	}
	return string(data)
}

// getHeadCommitMessage clones the bare repo and returns the HEAD commit message.
func getHeadCommitMessage(t *testing.T, bareDir string) string {
	t.Helper()
	cloneDir := t.TempDir()
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL:           bareDir,
		ReferenceName: plumbing.Main,
		SingleBranch:  true,
	})
	if err != nil {
		t.Fatalf("clone bare repo: %v", err)
	}

	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("get HEAD: %v", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	return commit.Message
}

// fileExistsInBare checks if a file exists in the HEAD of the bare repo.
func fileExistsInBare(t *testing.T, bareDir, filePath string) bool {
	t.Helper()
	cloneDir := t.TempDir()
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL:           bareDir,
		ReferenceName: plumbing.Main,
		SingleBranch:  true,
	})
	if err != nil {
		t.Fatalf("clone bare repo: %v", err)
	}

	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("get HEAD: %v", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("get tree: %v", err)
	}

	_, err = tree.File(filePath)
	return err == nil
}

// getAllFilesInBare returns all file paths in HEAD of the bare repo.
func getAllFilesInBare(t *testing.T, bareDir string) []string {
	t.Helper()
	cloneDir := t.TempDir()
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL:           bareDir,
		ReferenceName: plumbing.Main,
		SingleBranch:  true,
	})
	if err != nil {
		t.Fatalf("clone bare repo: %v", err)
	}

	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("get HEAD: %v", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("get tree: %v", err)
	}

	var files []string
	err = tree.Files().ForEach(func(f *object.File) error {
		files = append(files, f.Name)
		return nil
	})
	if err != nil {
		t.Fatalf("iterate files: %v", err)
	}
	return files
}

func TestGoGitPush_WritesManifestsToCorrectPath(t *testing.T) {
	bareDir := initBareRepo(t)
	pusher := newLocalPusher(t, bareDir)
	ctx := context.Background()

	manifests := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: my-app\n"

	sha, err := pusher.Push(ctx, services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "production",
		Manifests:     manifests,
		BuildID:       "build-001",
		ReleaseID:     "rel-001",
		CorrelationID: "corr-001",
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	if sha == "" {
		t.Fatal("expected non-empty commit SHA")
	}

	// Verify the file is at {environment}/{projectID}/manifests.yaml
	content := cloneBareAndReadFile(t, bareDir, "production/my-project/manifests.yaml")
	if content != manifests {
		t.Errorf("manifest content mismatch:\ngot:  %q\nwant: %q", content, manifests)
	}
}

func TestGoGitPush_IdempotentOnSameContent(t *testing.T) {
	bareDir := initBareRepo(t)
	pusher := newLocalPusher(t, bareDir)
	ctx := context.Background()

	opts := services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "staging",
		Manifests:     "apiVersion: v1\nkind: ConfigMap\n",
		BuildID:       "build-002",
		ReleaseID:     "rel-002",
		CorrelationID: "corr-002",
	}

	sha1, err := pusher.Push(ctx, opts)
	if err != nil {
		t.Fatalf("first Push failed: %v", err)
	}

	sha2, err := pusher.Push(ctx, opts)
	if err != nil {
		t.Fatalf("second Push failed: %v", err)
	}

	if sha1 != sha2 {
		t.Errorf("expected same SHA for idempotent push: got %s and %s", sha1, sha2)
	}
}

func TestGoGitPush_CommitHasTrailers(t *testing.T) {
	bareDir := initBareRepo(t)
	pusher := newLocalPusher(t, bareDir)
	ctx := context.Background()

	_, err := pusher.Push(ctx, services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "production",
		Manifests:     "apiVersion: v1\nkind: Service\n",
		BuildID:       "build-xyz",
		ReleaseID:     "rel-xyz",
		CorrelationID: "corr-xyz",
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	msg := getHeadCommitMessage(t, bareDir)

	expectedTrailers := []string{
		"Release-ID: rel-xyz",
		"Correlation-ID: corr-xyz",
		"Build-ID: build-xyz",
	}
	for _, trailer := range expectedTrailers {
		if !strings.Contains(msg, trailer) {
			t.Errorf("commit message missing trailer %q\ngot: %s", trailer, msg)
		}
	}
}

func TestGoGitPush_RejectsEmptyManifests(t *testing.T) {
	bareDir := initBareRepo(t)
	pusher := newLocalPusher(t, bareDir)
	ctx := context.Background()

	_, err := pusher.Push(ctx, services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "production",
		Manifests:     "",
		BuildID:       "build-001",
		ReleaseID:     "rel-001",
		CorrelationID: "corr-001",
	})
	if err == nil {
		t.Fatal("expected error for empty manifests")
	}
}

func TestGoGitRemove_LeavesGitkeep(t *testing.T) {
	bareDir := initBareRepo(t)
	pusher := newLocalPusher(t, bareDir)
	ctx := context.Background()

	// First, push manifests for a project
	_, err := pusher.Push(ctx, services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "production",
		Manifests:     "apiVersion: v1\nkind: Pod\n",
		BuildID:       "build-rm",
		ReleaseID:     "rel-rm",
		CorrelationID: "corr-rm",
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Remove the project
	sha, err := pusher.Remove(ctx, services.GitOpsRemoveOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "production",
		CorrelationID: "corr-rm-2",
	})
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty commit SHA from Remove")
	}

	// Verify .gitkeep exists in the environment directory
	if !fileExistsInBare(t, bareDir, "production/.gitkeep") {
		t.Error("expected .gitkeep in production/ after Remove")
	}

	// Verify manifests.yaml is gone
	if fileExistsInBare(t, bareDir, "production/my-project/manifests.yaml") {
		t.Error("expected manifests.yaml to be removed")
	}
}

func TestGoGitRemove_IdempotentWhenAlreadyRemoved(t *testing.T) {
	bareDir := initBareRepo(t)
	pusher := newLocalPusher(t, bareDir)
	ctx := context.Background()

	// Push then remove
	_, err := pusher.Push(ctx, services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "staging",
		Manifests:     "apiVersion: v1\nkind: Pod\n",
		BuildID:       "build-x",
		ReleaseID:     "rel-x",
		CorrelationID: "corr-x",
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	sha1, err := pusher.Remove(ctx, services.GitOpsRemoveOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "staging",
		CorrelationID: "corr-x-2",
	})
	if err != nil {
		t.Fatalf("first Remove failed: %v", err)
	}

	// Remove again — should succeed idempotently
	sha2, err := pusher.Remove(ctx, services.GitOpsRemoveOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "staging",
		CorrelationID: "corr-x-3",
	})
	if err != nil {
		t.Fatalf("second Remove failed: %v", err)
	}

	if sha1 != sha2 {
		t.Errorf("expected same SHA for idempotent remove: got %s and %s", sha1, sha2)
	}
}

func TestGoGitPush_UpdatesExistingManifests(t *testing.T) {
	bareDir := initBareRepo(t)
	pusher := newLocalPusher(t, bareDir)
	ctx := context.Background()

	// Push v1
	_, err := pusher.Push(ctx, services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "production",
		Manifests:     "version: 1\n",
		BuildID:       "build-v1",
		ReleaseID:     "rel-v1",
		CorrelationID: "corr-v1",
	})
	if err != nil {
		t.Fatalf("Push v1 failed: %v", err)
	}

	// Push v2 (different content)
	sha2, err := pusher.Push(ctx, services.GitOpsPushOpts{
		TenantID:      "tenant-abc",
		ProjectID:     "my-project",
		Environment:   "production",
		Manifests:     "version: 2\n",
		BuildID:       "build-v2",
		ReleaseID:     "rel-v2",
		CorrelationID: "corr-v2",
	})
	if err != nil {
		t.Fatalf("Push v2 failed: %v", err)
	}

	if sha2 == "" {
		t.Fatal("expected non-empty SHA for update push")
	}

	content := cloneBareAndReadFile(t, bareDir, "production/my-project/manifests.yaml")
	if content != "version: 2\n" {
		t.Errorf("expected updated content, got: %q", content)
	}
}
