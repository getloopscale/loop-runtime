// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/loopzero/loopzero/api/internal/gitclient"
)

// GoGitSourceStore implements SourceStore using the go-git library for all
// git operations except `git bundle create` (which go-git doesn't support).
// This eliminates shell injection risk by avoiding exec.Command for git.
type GoGitSourceStore struct {
	repo *gitclient.GiteaRepoManager
	git  *gitclient.GitRunner // only for bundle create
	auth *gogithttp.BasicAuth
}

// GoGitSourceStoreConfig holds configuration for the go-git source store.
type GoGitSourceStoreConfig struct {
	BaseURL string
	Token   string
	OrgName string
}

// NewGoGitSourceStore creates a go-git backed SourceStore.
func NewGoGitSourceStore(cfg GoGitSourceStoreConfig) (*GoGitSourceStore, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("gogit source store: base URL is required")
	}
	return &GoGitSourceStore{
		repo: gitclient.NewGiteaRepoManager(cfg.BaseURL, cfg.Token, cfg.OrgName, nil),
		git:  gitclient.NewGitRunner(cfg.Token),
		auth: &gogithttp.BasicAuth{Username: "loopzero-api", Password: cfg.Token},
	}, nil
}

// PushBundle pushes a source bundle to the Gitea repo.
func (s *GoGitSourceStore) PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (PushResult, error) {
	data, err := io.ReadAll(bundle)
	if err != nil {
		return PushResult{}, fmt.Errorf("gogit source: reading bundle: %w", err)
	}
	if len(data) == 0 {
		return PushResult{}, fmt.Errorf("gogit source: bundle is empty")
	}

	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return s.pushTarBundle(ctx, tenantID, projectID, data)
	}
	sourceID, err := s.pushGitBundle(ctx, tenantID, projectID, data)
	if err != nil {
		return PushResult{}, err
	}
	return PushResult{SourceID: sourceID}, nil
}

func (s *GoGitSourceStore) pushGitBundle(ctx context.Context, tenantID, projectID string, data []byte) (string, error) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.gogit.push_git",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", tenantID),
			attribute.String("loopzero.project", projectID),
		))
	defer span.End()

	tmpDir, err := os.MkdirTemp("", "loopzero-gogit-source-*")
	if err != nil {
		return "", fmt.Errorf("gogit source: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	bundlePath := filepath.Join(tmpDir, "source.bundle")
	if err := os.WriteFile(bundlePath, data, 0644); err != nil {
		return "", fmt.Errorf("gogit source: write bundle: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	repoName := sourceRepoName(tenantID, projectID)
	remoteURL := s.repo.RepoURL(repoName)

	if err := s.repo.EnsureRepo(ctx, repoName, false); err != nil {
		return "", fmt.Errorf("gogit source: ensure repo: %w", err)
	}

	// Clone existing repo via go-git (for incremental pushes)
	repo, err := gogit.PlainCloneContext(ctx, repoDir, false, &gogit.CloneOptions{
		URL:  remoteURL,
		Auth: s.auth,
	})
	if err != nil {
		// Clone failed — init fresh
		repo, err = gogit.PlainInit(repoDir, false)
		if err != nil {
			return "", fmt.Errorf("gogit source: init: %w", err)
		}
	}

	// Pull from bundle using git CLI (go-git doesn't support bundle protocol)
	if pullErr := s.git.Run(ctx, repoDir, "pull", bundlePath, "HEAD"); pullErr != nil {
		if fetchErr := s.git.Run(ctx, repoDir, "fetch", bundlePath, "HEAD"); fetchErr != nil {
			return "", fmt.Errorf("gogit source: fetch from bundle: %w (pull: %v)", fetchErr, pullErr)
		}
		if err := s.git.Run(ctx, repoDir, "checkout", "FETCH_HEAD"); err != nil {
			return "", fmt.Errorf("gogit source: checkout FETCH_HEAD: %w", err)
		}
	}

	// Get HEAD SHA
	head, err := repo.Head()
	if err != nil {
		// Fallback to CLI if go-git can't read HEAD after checkout
		sha, cliErr := s.git.Output(ctx, repoDir, "rev-parse", "HEAD")
		if cliErr != nil {
			return "", fmt.Errorf("gogit source: get HEAD: %w", err)
		}
		return s.pushToRemote(ctx, repo, repoDir, remoteURL, sha)
	}

	return s.pushToRemote(ctx, repo, repoDir, remoteURL, head.Hash().String())
}

func (s *GoGitSourceStore) pushToRemote(ctx context.Context, repo *gogit.Repository, repoDir, remoteURL, commitSHA string) (string, error) {
	// Ensure remote points to Gitea
	_ = repo.DeleteRemote("origin")
	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteURL},
	})
	if err != nil {
		return "", fmt.Errorf("gogit source: create remote: %w", err)
	}

	// Push via go-git
	if err := repo.PushContext(ctx, &gogit.PushOptions{
		RemoteName: "origin",
		Auth:       s.auth,
		Force:      true,
	}); err != nil && err != gogit.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("gogit source: push: %w", err)
	}

	return commitSHA, nil
}

func (s *GoGitSourceStore) pushTarBundle(ctx context.Context, tenantID, projectID string, data []byte) (PushResult, error) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.gogit.push_tar",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", tenantID),
			attribute.String("loopzero.project", projectID),
		))
	defer span.End()

	tmpDir, err := os.MkdirTemp("", "loopzero-gogit-tar-source-*")
	if err != nil {
		return PushResult{}, fmt.Errorf("gogit source: create temp dir: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: mkdir repo: %w", err)
	}

	// Extract tar.gz with strict validation
	tarPath := filepath.Join(tmpDir, "source.tar.gz")
	if err := os.WriteFile(tarPath, data, 0644); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: write tar: %w", err)
	}
	if _, err := SafeExtractTarGz(tarPath, repoDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: extract tar: %w", err)
	}

	contentHash, err := computeDirHashGogit(repoDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: content hash: %w", err)
	}

	repoName := sourceRepoName(tenantID, projectID)
	remoteURL := s.repo.RepoURL(repoName)

	if err := s.repo.EnsureRepo(ctx, repoName, false); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: ensure repo: %w", err)
	}

	// Init + add + commit via go-git
	repo, err := gogit.PlainInit(repoDir, false)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: init: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: worktree: %w", err)
	}

	if err := wt.AddGlob("."); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: add: %w", err)
	}

	_, err = wt.Commit(fmt.Sprintf("source upload ch-%s", contentHash[:12]), &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "LoopZero API",
			Email: "loopzero-api@loopzero.io",
			When:  time.Now(),
		},
	})
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: commit: %w", err)
	}

	// Push via go-git
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteURL},
	})
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: create remote: %w", err)
	}

	if err := repo.PushContext(ctx, &gogit.PushOptions{
		RemoteName: "origin",
		Auth:       s.auth,
		Force:      true,
	}); err != nil && err != gogit.NoErrAlreadyUpToDate {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: push: %w", err)
	}

	// Create git bundle for S3 (go-git doesn't support bundle create)
	bundlePath := filepath.Join(tmpDir, "source.bundle")
	if err := s.git.Run(ctx, repoDir, "bundle", "create", bundlePath, "HEAD"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gogit source: bundle create: %w", err)
	}

	_ = os.RemoveAll(repoDir)

	return PushResult{
		SourceID:      "ch-" + contentHash,
		GitBundlePath: bundlePath,
	}, nil
}

// computeDirHashGogit is a copy of computeDirHash for the gogit store.
// Kept as a separate function to avoid coupling to gitea_store.go internals.
func computeDirHashGogit(dir string) (string, error) {
	var paths []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, rel := range paths {
		h.Write([]byte(rel))
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
