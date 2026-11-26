// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/getloopscale/loop-runtime/api/internal/gitclient"
	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

// Plan v1.2 T1.2 (cutover-deep-review-hardening): DNS-1123 validation for
// tenantID/projectID before flowing into git repo names. Prevents path
// traversal, null-byte injection, Unicode/uppercase repo name collision,
// and length overflow. Pattern matches K8s namespace + label + Gitea repo
// + ArgoCD App name conventions (single canonical regex across stack).
//
// EC-1 (v1.1): legacy IDs in test fixtures audited via T1.2.0 pre-flight;
// any non-conforming names renamed before this regex shipped.
var (
	tenantIDRE  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	projectIDRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// ErrInvalidTenantOrProject is the sentinel returned by sourceRepoName when
// tenantID or projectID fails DNS-1123 validation. Route handlers in
// internal/routes/source.go use errors.Is to map this to HTTP 400 with code
// INVALID_TENANT_OR_PROJECT (Plan v1.2 T1.2).
var ErrInvalidTenantOrProject = errors.New("invalid tenant or project ID")

// GiteaSourceStoreConfig holds configuration for connecting to Gitea.
type GiteaSourceStoreConfig struct {
	BaseURL string // e.g., "http://gitea-http.loop-runtime-system.svc:3000"
	Token   string // Gitea access token for push authentication
	OrgName string // e.g., "loop-runtime"
}

// GiteaSourceStore implements SourceStore by pushing git bundles to Gitea
// source repos. The CLI creates a git bundle (git bundle create -1 HEAD),
// uploads it to the API, and this store pushes it to Gitea so the Argo
// Workflow can clone it (spec §7.1, Mode B).
type GiteaSourceStore struct {
	git  *gitclient.GitRunner
	repo *gitearepo.Client
}

// sourceRepoOptions creates a source repository empty: the pushed bundle
// brings its own history and branch, and an auto-initialised commit would
// be an unrelated root the first push has to reconcile.
var sourceRepoOptions = gitearepo.EnsureOptions{AutoInit: false}

// NewGiteaSourceStore creates a new Gitea-backed SourceStore.
func NewGiteaSourceStore(cfg GiteaSourceStoreConfig) (*GiteaSourceStore, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("gitea source store: base URL is required")
	}
	return &GiteaSourceStore{
		git:  gitclient.NewGitRunner(cfg.Token),
		repo: gitearepo.New(cfg.BaseURL, cfg.Token, cfg.OrgName, nil),
	}, nil
}

// sourceRepoName returns the deterministic Gitea repo name per spec §7.5.
//
// Plan v1.2 T1.2: tenantID and projectID MUST match DNS-1123 (1-63 chars,
// lowercase alphanumeric + hyphens, no leading/trailing hyphen). Returns
// error on invalid input — caller in routes/source.go maps to HTTP 400
// with code INVALID_TENANT_OR_PROJECT.
func sourceRepoName(tenantID, projectID string) (string, error) {
	if !tenantIDRE.MatchString(tenantID) {
		return "", fmt.Errorf("%w: tenantID must match DNS-1123 lowercase alphanumeric+hyphens, 1-63 chars (got %q)", ErrInvalidTenantOrProject, tenantID)
	}
	if projectID != "" {
		if !projectIDRE.MatchString(projectID) {
			return "", fmt.Errorf("%w: projectID must match DNS-1123 lowercase alphanumeric+hyphens, 1-63 chars (got %q)", ErrInvalidTenantOrProject, projectID)
		}
		return fmt.Sprintf("loop-runtime-source-%s-%s", tenantID, projectID), nil
	}
	return fmt.Sprintf("loop-runtime-source-%s", tenantID), nil
}

// PushBundle receives a source bundle from the CLI (git bundle or tar.gz),
// detects the format, and pushes to the Gitea source repo.
// Returns PushResult with SourceID and optionally a GitBundlePath for S3.
//
// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T3.2 (closes loop-console#31):
// PushBundle now returns typed *PushBundleError for each failure mode so
// SourceHandler.Upload can map to a specific actionable Message instead of
// the misleading legacy "Ensure your git history is clean" string. Downstream
// helpers (pushGitBundle / pushTarBundle) continue to return untyped
// fmt.Errorf for backward-compat; PushBundle wraps their non-typed errors as
// ReasonRemoteRejectedPush at the outer boundary (the handler's gitea-race
// substring discriminator takes precedence over this for F-03 preservation).
//
// The size ceiling (MaxBundleBytes) is enforced via io.LimitReader+1 — when
// io.ReadAll returns more than MaxBundleBytes we know the underlying reader
// had >= MaxBundleBytes+1 bytes available, which is the canonical
// ReasonBundleTooLarge signal.
func (s *GiteaSourceStore) PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (PushResult, error) {
	// Cap the read at MaxBundleBytes+1 so an oversized payload is detected
	// without buffering the entire body. Per ReasonBundleTooLarge contract.
	limited := io.LimitReader(bundle, MaxBundleBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return PushResult{}, NewPushBundleError(
			ReasonArchiveReadError, err,
			"could not read the source archive from the upload stream",
		)
	}
	if len(data) > MaxBundleBytes {
		return PushResult{}, NewPushBundleError(
			ReasonBundleTooLarge, nil,
			fmt.Sprintf("bundle exceeds %d-byte limit", MaxBundleBytes),
		)
	}
	if len(data) == 0 {
		return PushResult{}, NewPushBundleError(
			ReasonBundleMalformed, nil,
			"the uploaded bundle is empty",
		)
	}

	// Detect bundle format by magic bytes:
	//   tar.gz: 0x1f 0x8b (gzip magic)
	//   git bundle: "# v2 git bundle\n" or "# v3 git bundle\n"
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		result, err := s.pushTarBundle(ctx, tenantID, projectID, data)
		if err != nil {
			return PushResult{}, wrapDownstreamPushError(err)
		}
		return result, nil
	}
	sourceID, err := s.pushGitBundle(ctx, tenantID, projectID, data)
	if err != nil {
		return PushResult{}, wrapDownstreamPushError(err)
	}
	return PushResult{SourceID: sourceID}, nil
}

// wrapDownstreamPushError lifts an untyped error returned by pushGitBundle /
// pushTarBundle into a typed *PushBundleError so SourceHandler.Upload sees a
// uniform contract at the outer boundary.
//
// EC-3 absorption: ErrInvalidTenantOrProject (sentinel) is preserved as-is —
// PushBundle itself never owns the tenant-name validation, so the handler's
// pre-existing `errors.Is(err, source.ErrInvalidTenantOrProject)` ladder
// discriminator continues to short-circuit BEFORE the typed-error mapping.
// gitea-race substring matches are also preserved unchanged for F-03 —
// IsGiteaRaceCondition is consulted by the handler ladder BEFORE this typed
// wrap is interpreted.
func wrapDownstreamPushError(err error) error {
	if err == nil {
		return nil
	}
	// Preserve the sentinel chain so errors.Is(err, ErrInvalidTenantOrProject)
	// keeps working at the handler.
	if errors.Is(err, ErrInvalidTenantOrProject) {
		return err
	}
	msg := err.Error()
	class := classifyPushError(err)

	// 1. Network class (Sprint 4 — unchanged): backend unreachable (DNS / TCP
	//    refused / TLS / timeout). Detail is intentionally FIXED to avoid
	//    leaking internal DNS / addresses to the operator UI (scrub.Text
	//    would partially redact them anyway). The verbatim cause remains
	//    available via errors.Unwrap for operator logs (see source.go handler).
	//    Sprint 5 ADR D3 composition (verbatim cause in Detail) applies to the
	//    other 4 buckets below; the network bucket is the deliberate exception.
	if class == pushErrNetwork {
		return NewPushBundleError(ReasonSourceBackendUnreachable, err,
			"source backend is unreachable (network or DNS failure)")
	}
	// 2. Auth class (Sprint 5 — NEW per ADR D2): 401 / 403 / authentication-
	//    required / permission-denied patterns. The detail preserves the
	//    verbatim cause for the operator's diagnosis.
	if class == pushErrAuth {
		return NewPushBundleError(ReasonSourceBackendAuthFailure, err, msg)
	}
	// 3. Capacity class (Sprint 5 — NEW): 507 / quota-exceeded / no-space-left
	//    patterns. The detail preserves the verbatim cause.
	if class == pushErrCapacity {
		return NewPushBundleError(ReasonSourceBackendCapacity, err, msg)
	}
	// 4. Genuine remote-rejected (Sprint 5 — NARROWED per ADR D2): only the
	//    literal git "pre-receive hook declined" pattern reaches this bucket.
	//    The handler's gitea-race substring check still intercepts generic
	//    "remote rejected" / "cannot lock ref" / "stale info" patterns at the
	//    DEPLOY_IN_PROGRESS 409 ladder entry BEFORE pushErr.Reason is consulted
	//    (EC-3 preserved).
	if class == pushErrRemoteRejected {
		return NewPushBundleError(ReasonRemoteRejectedPush, err, msg)
	}
	// 5. Default mapping (Sprint 5 — CHANGED per ADR D2): unknown downstream.
	//    Sprint 4 incorrectly defaulted here to ReasonRemoteRejectedPush, which
	//    caused QA Red Team #33 — operators saw "Verify your project name is
	//    unused" on fresh names where no collision existed. The new catchall
	//    preserves the verbatim cause in Detail so the operator sees what
	//    actually went wrong rather than a fabricated specific diagnosis.
	return NewPushBundleError(ReasonUnknownDownstream, err, msg)
}

// pushErrClass is the bucket a push failure falls in.
type pushErrClass int

const (
	pushErrUnknown pushErrClass = iota
	pushErrNetwork
	pushErrAuth
	pushErrCapacity
	pushErrRemoteRejected
)

// classifyPushError decides the bucket from typed information first — a
// net.Error, a timeout, a Gitea HTTP status, git's own stderr — and only then
// from the message text, with patterns anchored to whole words and to a
// status context, so digits inside a SHA or "eof" inside a word decide
// nothing.
func classifyPushError(err error) pushErrClass {
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return pushErrNetwork
	}
	var status *gitearepo.StatusError
	if errors.As(err, &status) {
		switch status.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return pushErrAuth
		case http.StatusInsufficientStorage:
			return pushErrCapacity
		}
		return pushErrUnknown
	}
	text := err.Error()
	var cmdErr *gitclient.CommandError
	if errors.As(err, &cmdErr) {
		text = cmdErr.Output
	}
	return classifyPushText(strings.ToLower(text))
}

var (
	networkTextRe = regexp.MustCompile(`connection refused|no such host|i/o timeout|tls handshake|` +
		`context deadline exceeded|dial tcp|could not resolve host|failed to connect|\bunexpected eof\b|\beof\b`)
	authTextRe = regexp.MustCompile(`(?:status|returned|error:?|code)\s*:?\s*40[13]\b|` +
		`authentication (?:required|failed)|authorization failed|permission denied|\bforbidden\b|\bunauthorized\b`)
	capacityTextRe = regexp.MustCompile(`(?:status|returned|error:?|code)\s*:?\s*507\b|` +
		`quota exceeded|no space left|insufficient storage|disk full`)
	remoteRejectedTextRe = regexp.MustCompile(`pre-receive hook (?:declined|failed)`)
)

// classifyPushText is the last resort for an error that carries no type.
// "pre-receive hook" is the only remote-rejected signature: generic
// "remote rejected" / "cannot lock ref" stay with the route handler's
// DEPLOY_IN_PROGRESS ladder (EC-3).
func classifyPushText(lower string) pushErrClass {
	switch {
	case networkTextRe.MatchString(lower):
		return pushErrNetwork
	case authTextRe.MatchString(lower):
		return pushErrAuth
	case capacityTextRe.MatchString(lower):
		return pushErrCapacity
	case remoteRejectedTextRe.MatchString(lower):
		return pushErrRemoteRejected
	}
	return pushErrUnknown
}

// pushGitBundle handles git bundle format (original flow).
func (s *GiteaSourceStore) pushGitBundle(ctx context.Context, tenantID, projectID string, data []byte) (string, error) {
	ctx, span := otel.Tracer("loop-runtime-api").Start(ctx, "loop-runtime.api.source.push_git",
		trace.WithAttributes(
			attribute.String("loop-runtime.tenant", tenantID),
			attribute.String("loop-runtime.project", projectID),
		))
	defer span.End()

	tmpDir, err := os.MkdirTemp("", "loop-runtime-source-*")
	if err != nil {
		return "", fmt.Errorf("gitea source: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	bundlePath := filepath.Join(tmpDir, "source.bundle")
	// G703 false positive: bundlePath is tmpdir + literal filename "source.bundle",
	// no user input flows into the path.
	if err := os.WriteFile(bundlePath, data, 0o600); err != nil { //nolint:gosec // G703 false positive: tmpdir-internal path
		return "", fmt.Errorf("gitea source: write bundle: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	repoName, err := sourceRepoName(tenantID, projectID)
	if err != nil {
		return "", fmt.Errorf("gitea source: %w", err)
	}
	remoteURL := s.repo.RepoURL(repoName)

	if err := s.repo.EnsureRepo(ctx, repoName, sourceRepoOptions); err != nil {
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

	_ = s.git.Run(ctx, repoDir, "config", "user.email", "loop-runtime-api@runtime.getloopscale.com")
	_ = s.git.Run(ctx, repoDir, "config", "user.name", "LoopRuntime API")

	if err := s.git.RunWithAuth(ctx, repoDir, "push", "--force", "origin", "HEAD:refs/heads/main"); err != nil {
		return "", fmt.Errorf("gitea source: git push: %w", err)
	}

	return commitSHA, nil
}

// pushTarBundle handles tar.gz format for projects without git.
// Extracts the archive, computes a content hash, creates a git repo,
// commits all files, pushes to Gitea, and creates a git bundle for S3.
func (s *GiteaSourceStore) pushTarBundle(ctx context.Context, tenantID, projectID string, data []byte) (PushResult, error) {
	ctx, span := otel.Tracer("loop-runtime-api").Start(ctx, "loop-runtime.api.source.push_tar",
		trace.WithAttributes(
			attribute.String("loop-runtime.tenant", tenantID),
			attribute.String("loop-runtime.project", projectID),
		))
	defer span.End()

	// tmpDir is NOT deferred for cleanup — the git bundle file lives inside it.
	// The caller is responsible for cleaning up via PushResult.GitBundlePath's parent dir.
	tmpDir, err := os.MkdirTemp("", "loop-runtime-tar-source-*")
	if err != nil {
		return PushResult{}, fmt.Errorf("gitea source: create temp dir: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: mkdir repo: %w", err)
	}

	// SEC: Extract tar.gz with strict validation — no symlinks, no path traversal,
	// no absolute paths, decompressed size limit, file count limit.
	tarPath := filepath.Join(tmpDir, "source.tar.gz")
	// G703 false positive: tarPath is tmpdir + literal filename "source.tar.gz",
	// no user input flows into the path.
	if err := os.WriteFile(tarPath, data, 0o600); err != nil { //nolint:gosec // G703 false positive: tmpdir-internal path
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

	repoName, err := sourceRepoName(tenantID, projectID)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: %w", err)
	}
	remoteURL := s.repo.RepoURL(repoName)

	if err := s.repo.EnsureRepo(ctx, repoName, sourceRepoOptions); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: ensure repo: %w", err)
	}

	if err := s.git.Run(ctx, repoDir, "init"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return PushResult{}, fmt.Errorf("gitea source: git init: %w", err)
	}
	_ = s.git.Run(ctx, repoDir, "config", "user.email", "loop-runtime-api@runtime.getloopscale.com")
	_ = s.git.Run(ctx, repoDir, "config", "user.name", "LoopRuntime API")
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
		// G304 false positive: `rel` came from filepath.Walk(dir) +
		// filepath.Rel(dir, p), so the resolved path is always within
		// `dir`. Caller passes a tmpdir, never user input.
		data, err := os.ReadFile(filepath.Join(dir, rel)) //nolint:gosec // G304 false positive: rel originated from filepath.Walk(dir)
		if err != nil {
			return "", err
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
