// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/gitclient"
	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

// These tests run the pusher against a local bare repository with the real git
// binary, so they need no Gitea: the repo manager is replaced by one that
// resolves a tenant repo to a directory on disk.

type localRepoManager struct {
	root      string
	ensureErr error
	// ensured records the options of every EnsureRepo call, when non-nil.
	ensured *[]gitearepo.EnsureOptions
}

func (m localRepoManager) EnsureRepo(_ context.Context, _ string, opts gitearepo.EnsureOptions) error {
	if m.ensured != nil {
		*m.ensured = append(*m.ensured, opts)
	}
	return m.ensureErr
}
func (m localRepoManager) RepoURL(repoName string) string {
	return filepath.Join(m.root, repoName+".git")
}

// newLocalRemote creates <root>/<tenant>-gitops.git seeded with the given files
// on branch main, and returns root.
func newLocalRemote(t *testing.T, tenant string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, tenant+"-gitops.git")
	gitRun(t, root, "git", "init", "--bare", "--initial-branch=main", bare)
	if len(files) == 0 {
		return root
	}
	seed := filepath.Join(t.TempDir(), "seed")
	gitRun(t, root, "git", "clone", bare, seed)
	for rel, content := range files {
		p := filepath.Join(seed, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, seed, "git", "add", ".")
	gitRun(t, seed, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "seed")
	gitRun(t, seed, "git", "push", "origin", "HEAD:refs/heads/main")
	return root
}

func newLocalPusher(repos gitRepoManager, runner gitRunner) *GiteaGitOpsPusher {
	if runner == nil {
		runner = gitclient.NewGitRunner("")
	}
	return &GiteaGitOpsPusher{git: runner, repo: repos}
}

// remoteFiles lists the files on main of the bare repo.
func remoteFiles(t *testing.T, root, tenant string) []string {
	t.Helper()
	out := gitRun(t, root, "git", "--git-dir", filepath.Join(root, tenant+"-gitops.git"),
		"ls-tree", "-r", "--name-only", "main")
	return strings.Fields(out)
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestRemoveFailsWhenTheRepositoryCannotBeCloned(t *testing.T) {
	// Arrange: the repo URL resolves to nothing, as during a Gitea outage.
	pusher := newLocalPusher(localRepoManager{root: t.TempDir()}, nil)

	// Act
	_, err := pusher.Remove(context.Background(), deploycontracts.GitOpsRemoveOpts{
		TenantID: "acme", ProjectID: "web", Environment: "production",
	})

	// Assert: a delete that could not reach the repo must not report success.
	if err == nil {
		t.Fatal("Remove returned nil although the GitOps repo could not be cloned")
	}
	if !strings.Contains(err.Error(), "clone") {
		t.Errorf("error %q does not say the clone failed", err)
	}
}

func TestPushFailsWhenTheRepositoryCannotBeCloned(t *testing.T) {
	pusher := newLocalPusher(localRepoManager{root: t.TempDir()}, nil)

	_, err := pusher.Push(context.Background(), deploycontracts.GitOpsPushOpts{
		TenantID: "acme", ProjectID: "web", Environment: "production", Manifests: testManifests,
	})

	if err == nil {
		t.Fatal("Push returned nil although the GitOps repo could not be cloned")
	}
}

func TestRemoveDeletesOnlyTheProjectAndKeepsTheEnvironmentPath(t *testing.T) {
	// Arrange
	root := newLocalRemote(t, "acme", map[string]string{
		"production/web/manifests.yaml": testManifests,
		"production/api/manifests.yaml": testManifests,
	})
	pusher := newLocalPusher(localRepoManager{root: root}, nil)

	// Act
	sha, err := pusher.Remove(context.Background(), deploycontracts.GitOpsRemoveOpts{
		TenantID: "acme", ProjectID: "web", Environment: "production",
	})

	// Assert
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("sha = %q, want a commit SHA", sha)
	}
	files := remoteFiles(t, root, "acme")
	if contains(files, "production/web/manifests.yaml") {
		t.Errorf("removed project still on main: %v", files)
	}
	if !contains(files, "production/api/manifests.yaml") || !contains(files, "production/.gitkeep") {
		t.Errorf("sibling project or env .gitkeep missing: %v", files)
	}
}

func TestRemoveOfAnAbsentProjectReturnsTheCurrentHead(t *testing.T) {
	root := newLocalRemote(t, "acme", map[string]string{"production/api/manifests.yaml": testManifests})
	pusher := newLocalPusher(localRepoManager{root: root}, nil)
	head := gitRun(t, root, "git", "--git-dir", filepath.Join(root, "acme-gitops.git"), "rev-parse", "main")

	sha, err := pusher.Remove(context.Background(), deploycontracts.GitOpsRemoveOpts{
		TenantID: "acme", ProjectID: "web", Environment: "production",
	})

	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if sha != head {
		t.Errorf("sha = %q, want current head %q", sha, head)
	}
}

func TestPushWritesManifestsAndIsIdempotent(t *testing.T) {
	// Arrange: an empty repository, as right after it is created.
	root := newLocalRemote(t, "acme", nil)
	pusher := newLocalPusher(localRepoManager{root: root}, nil)
	opts := deploycontracts.GitOpsPushOpts{
		TenantID: "acme", ProjectID: "web", Environment: "production", Manifests: testManifests,
		ReleaseID: "rel-1", CorrelationID: "corr-1", BuildID: "build-1",
	}

	// Act
	first, err := pusher.Push(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Push: %v", err)
	}
	second, err := pusher.Push(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Push: %v", err)
	}

	// Assert
	if first != second {
		t.Errorf("identical push produced a new commit: %s then %s", first, second)
	}
	if files := remoteFiles(t, root, "acme"); !contains(files, "production/web/manifests.yaml") {
		t.Errorf("manifests not on main: %v", files)
	}
}

// racingRunner pushes a competing commit to the remote right before the first
// push of the pusher under test, forcing the rebase-and-retry path.
type racingRunner struct {
	*gitclient.GitRunner
	t      *testing.T
	remote string
	raced  bool
}

func (r *racingRunner) RunWithAuth(ctx context.Context, dir string, args ...string) error {
	if len(args) > 0 && args[0] == "push" && !r.raced {
		r.raced = true
		other := filepath.Join(r.t.TempDir(), "other")
		gitRun(r.t, filepath.Dir(other), "git", "clone", r.remote, other)
		if err := os.MkdirAll(filepath.Join(other, "staging", "api"), 0o750); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, "staging", "api", "manifests.yaml"), []byte(testManifests), 0o600); err != nil {
			r.t.Fatal(err)
		}
		gitRun(r.t, other, "git", "add", ".")
		gitRun(r.t, other, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "concurrent")
		gitRun(r.t, other, "git", "push", "origin", "HEAD:refs/heads/main")
	}
	return r.GitRunner.RunWithAuth(ctx, dir, args...)
}

func TestPushRebasesOverAConcurrentCommit(t *testing.T) {
	root := newLocalRemote(t, "acme", map[string]string{"production/api/manifests.yaml": testManifests})
	runner := &racingRunner{GitRunner: gitclient.NewGitRunner(""), t: t, remote: filepath.Join(root, "acme-gitops.git")}
	pusher := newLocalPusher(localRepoManager{root: root}, runner)

	_, err := pusher.Push(context.Background(), deploycontracts.GitOpsPushOpts{
		TenantID: "acme", ProjectID: "web", Environment: "production", Manifests: testManifests,
	})

	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	files := remoteFiles(t, root, "acme")
	if !contains(files, "production/web/manifests.yaml") || !contains(files, "staging/api/manifests.yaml") {
		t.Errorf("rebase lost a commit: %v", files)
	}
}

// The tenant GitOps repository must be auto-initialised on the branch every
// push targets: the workspace clones it before anything is pushed.
func TestOpenWorkspaceEnsuresAnAutoInitialisedRepoOnThePushBranch(t *testing.T) {
	// Arrange
	root := newLocalRemote(t, "acme", nil)
	var ensured []gitearepo.EnsureOptions
	pusher := newLocalPusher(localRepoManager{root: root, ensured: &ensured}, nil)

	// Act
	_, cleanup, err := pusher.openWorkspace(context.Background(), "acme", "push")
	if err != nil {
		t.Fatalf("openWorkspace: %v", err)
	}
	cleanup()

	// Assert
	want := gitearepo.EnsureOptions{AutoInit: true, DefaultBranch: gitopsBranch}
	if len(ensured) != 1 || ensured[0] != want {
		t.Errorf("EnsureRepo options = %+v, want exactly one call with %+v", ensured, want)
	}
}
