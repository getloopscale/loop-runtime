// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

const (
	gitopsBranch      = "main"
	gitopsPushRetries = 3
	gitopsCloneDepth  = "5"
)

// gitopsWorkspace is a shallow clone of one tenant GitOps repo in a private
// temp directory, with the commit identity set. Push and Remove are both
// "open, edit, commit if changed, push with rebase-retry" over it.
type gitopsWorkspace struct {
	git gitRunner
	dir string
	op  string // "push" | "remove" — prefixes every error
}

// tenantGitOpsRepoOptions auto-initialises the tenant GitOps repository on
// gitopsBranch: the workspace clones it before the first push, and every push
// targets that branch.
var tenantGitOpsRepoOptions = gitearepo.EnsureOptions{AutoInit: true, DefaultBranch: gitopsBranch}

// openWorkspace ensures the tenant repo exists and clones it. A clone failure
// is always returned: the repo is created before the clone, and cloning an
// EMPTY repo succeeds, so a failing clone means the repo could not be reached
// (network, auth, server error) — never "nothing there".
func (p *GiteaGitOpsPusher) openWorkspace(ctx context.Context, tenantID, op string) (*gitopsWorkspace, func(), error) {
	repoName := naming.TenantGitOpsRepoName(tenantID)
	if err := p.repo.EnsureRepo(ctx, repoName, tenantGitOpsRepoOptions); err != nil {
		return nil, nil, fmt.Errorf("gitops %s: ensure repo: %w", op, err)
	}
	tmpDir, err := os.MkdirTemp("", "loop-runtime-gitops-"+op+"-*")
	if err != nil {
		return nil, nil, fmt.Errorf("gitops %s: create temp dir: %w", op, err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	ws := &gitopsWorkspace{git: p.git, dir: filepath.Join(tmpDir, "repo"), op: op}
	// SEC-1: Auth via the runner's auth header, never embedded in the URL.
	if err := p.git.RunWithAuth(ctx, tmpDir, "clone", "--depth", gitopsCloneDepth, p.repo.RepoURL(repoName), ws.dir); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("gitops %s: clone %s: %w", op, repoName, err)
	}
	for _, kv := range [][2]string{{"user.email", "loop-runtime-api@runtime.getloopscale.com"}, {"user.name", "LoopRuntime API"}} {
		if err := p.git.Run(ctx, ws.dir, "config", kv[0], kv[1]); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("gitops %s: git config %s: %w", op, kv[0], err)
		}
	}
	return ws, cleanup, nil
}

func (w *gitopsWorkspace) writeFile(rel, content string) error {
	path := filepath.Join(w.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("gitops %s: create dir for %s: %w", w.op, rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("gitops %s: write %s: %w", w.op, rel, err)
	}
	return nil
}

// commitAndPush commits the staged changes and pushes them, returning the new
// HEAD. When nothing is staged it pushes nothing and returns the current HEAD,
// which makes both operations idempotent.
func (w *gitopsWorkspace) commitAndPush(ctx context.Context, message string) (string, error) {
	changed, err := w.hasStagedChanges(ctx)
	if err != nil {
		return "", err
	}
	if !changed {
		return w.head(ctx)
	}
	if err := w.git.Run(ctx, w.dir, "commit", "-m", message); err != nil {
		return "", fmt.Errorf("gitops %s: git commit: %w", w.op, err)
	}
	if err := w.pushWithRebaseRetry(ctx); err != nil {
		return "", err
	}
	return w.head(ctx)
}

// hasStagedChanges asks git itself (exit status of `diff --cached --quiet`)
// rather than matching "nothing to commit" in an error string.
func (w *gitopsWorkspace) hasStagedChanges(ctx context.Context) (bool, error) {
	err := w.git.Run(ctx, w.dir, "diff", "--cached", "--quiet")
	if err == nil {
		return false, nil
	}
	if gitExitCode(err) == 1 {
		return true, nil
	}
	return false, fmt.Errorf("gitops %s: inspect staged changes: %w", w.op, err)
}

// pushWithRebaseRetry pushes HEAD to main; a rejected push fetches and rebases
// onto the concurrent update and tries again. The last push error is returned.
func (w *gitopsWorkspace) pushWithRebaseRetry(ctx context.Context) error {
	var lastErr error
	for attempt := 1; attempt <= gitopsPushRetries; attempt++ {
		lastErr = w.git.RunWithAuth(ctx, w.dir, "push", "origin", "HEAD:refs/heads/"+gitopsBranch)
		if lastErr == nil {
			return nil
		}
		if attempt == gitopsPushRetries {
			break
		}
		if err := w.git.RunWithAuth(ctx, w.dir, "fetch", "origin"); err != nil {
			return fmt.Errorf("gitops %s: fetch after rejected push: %w", w.op, err)
		}
		if err := w.git.Run(ctx, w.dir, "rebase", "origin/"+gitopsBranch); err != nil {
			return fmt.Errorf("gitops %s: rebase after rejected push: %w", w.op, err)
		}
	}
	return fmt.Errorf("gitops %s: push failed after %d attempts: %w", w.op, gitopsPushRetries, lastErr)
}

// head returns the HEAD commit SHA, or "" for a repository with no commits
// yet (nothing has ever been deployed there).
func (w *gitopsWorkspace) head(ctx context.Context) (string, error) {
	sha, err := w.git.Output(ctx, w.dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err == nil {
		return sha, nil
	}
	if gitExitCode(err) == 1 {
		return "", nil
	}
	return "", fmt.Errorf("gitops %s: resolve HEAD: %w", w.op, err)
}

// gitExitCode returns the exit status of a failed git process, or -1 when the
// error is not a process exit (the binary could not run, the context ended).
func gitExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
