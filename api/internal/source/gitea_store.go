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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/loopzero/loopzero/api/internal/gitclient"
)

// GiteaSourceStoreConfig holds configuration for connecting to Gitea.
type GiteaSourceStoreConfig struct {
	BaseURL string // e.g., "http://gitea-http.loopzero-system.svc:3000"
	Token   string // Gitea access token for push authentication
	OrgName string // e.g., "loopzero"
}

// GiteaSourceStore implements SourceStore by pushing git bundles to Gitea
// source repos. The CLI creates a git bundle (git bundle create -1 HEAD),
// uploads it to the API, and this store pushes it to Gitea so the Argo
// Workflow can clone it (spec §7.1, Mode B).
type GiteaSourceStore struct {
	git  *gitclient.GitRunner
	repo *gitclient.GiteaRepoManager
}

// NewGiteaSourceStore creates a new Gitea-backed SourceStore.
func NewGiteaSourceStore(cfg GiteaSourceStoreConfig) (*GiteaSourceStore, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("gitea source store: base URL is required")
	}
	return &GiteaSourceStore{
		git:  gitclient.NewGitRunner(cfg.Token),
		repo: gitclient.NewGiteaRepoManager(cfg.BaseURL, cfg.Token, cfg.OrgName, nil),
	}, nil
}

// sourceRepoName returns the deterministic Gitea repo name per spec §7.5.
func sourceRepoName(tenantID, projectID string) string {
	if projectID != "" {
		return fmt.Sprintf("loopzero-source-%s-%s", tenantID, projectID)
	}
	return fmt.Sprintf("loopzero-source-%s", tenantID)
}

// PushBundle receives a source bundle from the CLI (git bundle or tar.gz),
// detects the format, and pushes to the Gitea source repo.
// Returns PushResult with SourceID and optionally a GitBundlePath for S3.
func (s *GiteaSourceStore) PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (PushResult, error) {
	data, err := io.ReadAll(bundle)
	if err != nil {
		return PushResult{}, fmt.Errorf("gitea source: reading bundle: %w", err)
	}
	if len(data) == 0 {
		return PushResult{}, fmt.Errorf("gitea source: bundle is empty")
	}

	// Detect bundle format by magic bytes:
	//   tar.gz: 0x1f 0x8b (gzip magic)
	//   git bundle: "# v2 git bundle\n" or "# v3 git bundle\n"
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return s.pushTarBundle(ctx, tenantID, projectID, data)
	}
	sourceID, err := s.pushGitBundle(ctx, tenantID, projectID, data)
	if err != nil {
		return PushResult{}, err
	}
	return PushResult{SourceID: sourceID}, nil
}

// pushGitBundle handles git bundle format (original flow).
func (s *GiteaSourceStore) pushGitBundle(ctx context.Context, tenantID, projectID string, data []byte) (string, error) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.push_git",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", tenantID),
			attribute.String("loopzero.project", projectID),
		))
	defer span.End()

	tmpDir, err := os.MkdirTemp("", "loopzero-source-*")
	if err != nil {
		return "", fmt.Errorf("gitea source: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	bundlePath := filepath.Join(tmpDir, "source.bundle")
	if err := os.WriteFile(bundlePath, data, 0644); err != nil {
		return "", fmt.Errorf("gitea source: write bundle: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	repoName := sourceRepoName(tenantID, projectID)
	remoteURL := s.repo.RepoURL(repoName)

	if err := s.repo.EnsureRepo(ctx, repoName, false); err != nil {
		return "", fmt.Errorf("gitea source: ensure repo: %w", err)
	}

	// Try to clone existing repo (for incremental pushes)
	if err := s.git.RunWithAuth(ctx, tmpDir, "clone", remoteURL, repoDir); err != nil {
		if err := s.git.Run(ctx, tmpDir, "init", repoDir); err != nil {
			return "", fmt.Errorf("gitea source: git init: %w", err)
		}
	}

	// Pull from bundle
	if err := s.git.Run(ctx, repoDir, "pull", bundlePath, "HEAD"); err != nil {
		if err2 := s.git.Run(ctx, repoDir, "fetch", bundlePath, "HEAD"); err2 != nil {
			return "", fmt.Errorf("gitea source: git fetch from bundle: %w (pull error: %w)", err2, err)
		}
		if err2 := s.git.Run(ctx, repoDir, "checkout", "FETCH_HEAD"); err2 != nil {
			return "", fmt.Errorf("gitea source: git checkout FETCH_HEAD: %w", err2)
		}
	}

	commitSHA, err := s.git.Output(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitea source: get HEAD SHA: %w", err)
	}

	_ = s.git.Run(ctx, repoDir, "remote", "remove", "origin")
	if err := s.git.Run(ctx, repoDir, "remote", "add", "origin", remoteURL); err != nil {
		return "", fmt.Errorf("gitea source: set remote: %w", err)
	}

	_ = s.git.Run(ctx, repoDir, "config", "user.email", "loopzero-api@loopzero.io")
	_ = s.git.Run(ctx, repoDir, "config", "user.name", "LoopZero API")

	if err := s.git.RunWithAuth(ctx, repoDir, "push", "--force", "origin", "HEAD:refs/heads/main"); err != nil {
		return "", fmt.Errorf("gitea source: git push: %w", err)
	}

	return commitSHA, nil
}

// pushTarBundle handles tar.gz format for projects without git.
// Extracts the archive, computes a content hash, creates a git repo,
// commits all files, pushes to Gitea, and creates a git bundle for S3.
func (s *GiteaSourceStore) pushTarBundle(ctx context.Context, tenantID, projectID string, data []byte) (PushResult, error) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.push_tar",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", tenantID),
			attribute.String("loopzero.project", projectID),
		))
	defer span.End()

	// tmpDir is NOT deferred for cleanup — the git bundle file lives inside it.
	// The caller is responsible for cleaning up via PushResult.GitBundlePath's parent dir.
	tmpDir, err := os.MkdirTemp("", "loopzero-tar-source-*")
	if err != nil {
		return PushResult{}, fmt.Errorf("gitea source: create temp dir: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: mkdir repo: %w", err)
	}

	// SEC: Extract tar.gz with strict validation — no symlinks, no path traversal,
	// no absolute paths, decompressed size limit, file count limit.
	tarPath := filepath.Join(tmpDir, "source.tar.gz")
	if err := os.WriteFile(tarPath, data, 0644); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: write tar: %w", err)
	}
	if _, err := SafeExtractTarGz(tarPath, repoDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: extract tar: %w", err)
	}

	// Compute content hash from extracted files
	contentHash, err := computeDirHash(repoDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: content hash: %w", err)
	}

	repoName := sourceRepoName(tenantID, projectID)
	remoteURL := s.repo.RepoURL(repoName)

	if err := s.repo.EnsureRepo(ctx, repoName, false); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: ensure repo: %w", err)
	}

	if err := s.git.Run(ctx, repoDir, "init"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: git init: %w", err)
	}
	_ = s.git.Run(ctx, repoDir, "config", "user.email", "loopzero-api@loopzero.io")
	_ = s.git.Run(ctx, repoDir, "config", "user.name", "LoopZero API")
	if err := s.git.Run(ctx, repoDir, "add", "-A"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: git add: %w", err)
	}
	if err := s.git.Run(ctx, repoDir, "commit", "-m", fmt.Sprintf("source upload ch-%s", contentHash[:12])); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: git commit: %w", err)
	}

	_ = s.git.Run(ctx, repoDir, "remote", "remove", "origin")
	if err := s.git.Run(ctx, repoDir, "remote", "add", "origin", remoteURL); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: set remote: %w", err)
	}
	if err := s.git.RunWithAuth(ctx, repoDir, "push", "--force", "origin", "HEAD:refs/heads/main"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: git push: %w", err)
	}

	// Create git bundle for S3 upload (CWT expects git bundle format).
	bundlePath := filepath.Join(tmpDir, "source.bundle")
	if err := s.git.Run(ctx, repoDir, "bundle", "create", bundlePath, "HEAD"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: create git bundle: %w", err)
	}

	// Clean up repo dir but keep the bundle and tmpDir
	_ = os.RemoveAll(repoDir)

	return PushResult{
		SourceID:      "ch-" + contentHash,
		GitBundlePath: bundlePath,
	}, nil
}

// computeDirHash computes a deterministic SHA256 hash of all files in a directory.
// Uses sorted paths and file contents for reproducibility.
func computeDirHash(dir string) (string, error) {
	var paths []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		// Skip .git directory only (not .github/, .gitignore, .gitattributes)
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

