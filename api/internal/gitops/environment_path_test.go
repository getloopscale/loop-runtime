// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import (
	"context"
	"testing"
)

// An environment with no release left its Argo CD Application in
// ComparisonError, "app path does not exist", until the first deploy
// (getloopscale/loopzero#134).
func TestEnsureEnvironmentPathCreatesTheDirectoryTheApplicationReads(t *testing.T) {
	root := newLocalRemote(t, "acme", map[string]string{"production/web/manifests.yaml": testManifests})
	pusher := newLocalPusher(localRepoManager{root: root}, nil)

	if err := pusher.EnsureEnvironmentPath(context.Background(), "acme", "web", "qa"); err != nil {
		t.Fatalf("EnsureEnvironmentPath: %v", err)
	}

	files := remoteFiles(t, root, "acme")
	if !contains(files, "qa/web/.gitkeep") {
		t.Errorf("qa/web/.gitkeep is not on main: %v", files)
	}
	if !contains(files, "production/web/manifests.yaml") {
		t.Errorf("another environment's manifests were touched: %v", files)
	}
}

func TestEnsureEnvironmentPathWorksOnARepositoryWithNoCommit(t *testing.T) {
	root := newLocalRemote(t, "acme", nil)
	pusher := newLocalPusher(localRepoManager{root: root}, nil)

	if err := pusher.EnsureEnvironmentPath(context.Background(), "acme", "web", "qa"); err != nil {
		t.Fatalf("EnsureEnvironmentPath: %v", err)
	}
	if files := remoteFiles(t, root, "acme"); !contains(files, "qa/web/.gitkeep") {
		t.Errorf("qa/web/.gitkeep is not on main: %v", files)
	}
}

func TestEnsureEnvironmentPathLeavesADeployedEnvironmentAlone(t *testing.T) {
	root := newLocalRemote(t, "acme", map[string]string{"qa/web/manifests.yaml": testManifests})
	pusher := newLocalPusher(localRepoManager{root: root}, nil)
	before := gitRun(t, root, "git", "--git-dir", root+"/acme-gitops.git", "rev-parse", "main")

	if err := pusher.EnsureEnvironmentPath(context.Background(), "acme", "web", "qa"); err != nil {
		t.Fatalf("EnsureEnvironmentPath: %v", err)
	}

	after := gitRun(t, root, "git", "--git-dir", root+"/acme-gitops.git", "rev-parse", "main")
	if before != after {
		t.Errorf("a commit was added to an environment that already has manifests: %s -> %s", before, after)
	}
	if files := remoteFiles(t, root, "acme"); contains(files, "qa/web/.gitkeep") {
		t.Errorf("a placeholder was written next to real manifests: %v", files)
	}
}

func TestEnsureEnvironmentPathFailsWhenTheRepositoryCannotBeCloned(t *testing.T) {
	pusher := newLocalPusher(localRepoManager{root: t.TempDir()}, nil)

	if err := pusher.EnsureEnvironmentPath(context.Background(), "acme", "web", "qa"); err == nil {
		t.Error("expected an error for a repository that cannot be cloned")
	}
}
