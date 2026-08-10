// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/getloopscale/loopzero/api/internal/archive"
	"github.com/getloopscale/loopzero/api/internal/gitclient"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// AutoDeployConfig holds configuration for auto-deploy on push.
//
// INVARIANT (cell-1-activation-plan T1.4 / ADR C11 path 3):
// Auto-deploy MUST NOT set tenant.cell_id from webhook payload. Cell
// placement is operator-only via POST /api/v1/admin/tenants/pin (see
// MEET-INFRA-NORMALIZE-T3-multi-cell C8 + admin_pin_cell.go). The webhook
// path inherits the tenant's CURRENT cell_id from the database — never
// overrides it. Drift here lets any GitHub push silently re-pin a tenant
// across cells, breaking placement layer's capacity gate semantics.
// Verified by: TestAdminPinCell_AuthzInvariantC11Path3_NoCellIDInWebhookPath
// (admin_pin_cell_test.go) + grep regression in cell2-activation-gates.sh
// gate_c11.
type AutoDeployConfig struct {
	AppClient      *AppClient
	SourceStore    SourcePusher
	BuildCanceller BuildCanceller
	ObjectStore    ObjectUploader
	BuildSubmitter BuildSubmitter
	SourceBucket   string // S3 bucket for source bundles (default: "loopzero-builds")
	Logger         *slog.Logger
}

// SourcePusher pushes source bundles (DIP — subset of source.SourceStore).
type SourcePusher interface {
	PushTarball(ctx context.Context, tenantID, projectName string, tarball io.Reader) (sourceID string, err error)
}

// ObjectUploader uploads objects to S3 (DIP — same as routes.ObjectUploader).
type ObjectUploader interface {
	Upload(ctx context.Context, bucket, key string, data io.Reader) error
}

// BuildSubmitter submits builds (DIP — subset of BuildOrchestrator).
type BuildSubmitter interface {
	SubmitBuildFromWebhook(ctx context.Context, req WebhookBuildRequest) error
}

// BuildCanceller cancels queued builds for an environment (DIP).
// Used to implement superseded-push: newer push cancels older QUEUED builds.
type BuildCanceller interface {
	CancelQueuedBuilds(ctx context.Context, tenantID, projectID, environment string) (int, error)
}

// WebhookBuildRequest contains everything needed to submit a build from a webhook push.
type WebhookBuildRequest struct {
	TenantID       string
	ProjectName    string
	Environment    string
	CommitSHA      string
	InstallationID int64
	RepoFullName   string
	Branch         string
	CommitMessage  string
	SenderLogin    string
	LoopZeroYaml       *LoopZeroYamlMinimal
}

// AutoDeployer handles auto-deploy on push events.
type AutoDeployer struct {
	cfg AutoDeployConfig
}

// NewAutoDeployer creates a new AutoDeployer.
func NewAutoDeployer(cfg AutoDeployConfig) *AutoDeployer {
	return &AutoDeployer{cfg: cfg}
}

// HandlePush processes a push event and triggers a deploy if the repo is linked.
// This is called asynchronously from the webhook handler.
func (d *AutoDeployer) HandlePush(ctx context.Context, repoFullName, branch, commitSHA string,
	installationID int64, tenantID, projectName, commitMessage, senderLogin string) error {

	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.github.autodeploy",
		trace.WithAttributes(
			attribute.String("loopzero.github.repo", repoFullName),
			attribute.String("loopzero.github.commit_sha", commitSHA),
		))
	defer span.End()

	log := d.cfg.Logger.With(
		"repo", repoFullName,
		"branch", branch,
		"sha", commitSHA,
		"project", projectName,
	)

	log.Info("auto-deploy: starting")

	// Step 1: Download source tarball from GitHub
	log.Info("auto-deploy: downloading source from GitHub")
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	tarballReader, err := d.cfg.AppClient.DownloadSource(ctx, installationID, owner, repo, commitSHA)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("auto-deploy: download source: %w", err)
	}
	defer func() { _ = tarballReader.Close() }()

	// Step 2: Save tarball to temp file (need to read twice: parse loopzero.yaml + upload)
	tmpFile, err := os.CreateTemp("", "loopzero-github-source-*.tar.gz")
	if err != nil {
		return fmt.Errorf("auto-deploy: create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	if _, err := io.Copy(tmpFile, tarballReader); err != nil {
		return fmt.Errorf("auto-deploy: save tarball: %w", err)
	}

	// Step 3: Parse loopzero.yaml from tarball
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("auto-deploy: seek: %w", err)
	}

	loopzeroYaml, err := extractLoopZeroYaml(tmpFile)
	if err != nil {
		log.Warn("auto-deploy: no loopzero.yaml found in repo, skipping", "error", err)
		return nil // not a LoopZero project — ignore silently
	}

	log.Info("auto-deploy: loopzero.yaml found", "project_in_yaml", loopzeroYaml.Project, "apps", len(loopzeroYaml.Apps))

	// Step 4: Convert tarball to git bundle (loopzero-clone CWT expects .bundle format)
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("auto-deploy: seek for conversion: %w", err)
	}
	bundlePath, err := tarballToGitBundle(ctx, tmpFile, commitSHA, commitMessage, senderLogin)
	if err != nil {
		return fmt.Errorf("auto-deploy: convert to git bundle: %w", err)
	}
	defer func() { _ = os.Remove(bundlePath) }()
	log.Info("auto-deploy: tarball converted to git bundle")

	// Step 5: Upload git bundle to S3 for build cluster
	if d.cfg.ObjectStore != nil {
		// G304 false positive: bundlePath comes from tarballToGitBundle
		// which returns a path under os.MkdirTemp — never user-controlled.
		bundleFile, err := os.Open(bundlePath) //nolint:gosec // G304 false positive: bundlePath is tmpdir-internal
		if err != nil {
			return fmt.Errorf("auto-deploy: open bundle: %w", err)
		}
		defer func() { _ = bundleFile.Close() }()
		objKey := fmt.Sprintf("%s/source/%s.bundle", tenantID, commitSHA)
		bucket := d.cfg.SourceBucket
		if bucket == "" {
			bucket = "loopzero-builds"
		}
		if err := d.cfg.ObjectStore.Upload(ctx, bucket, objKey, bundleFile); err != nil {
			return fmt.Errorf("auto-deploy: S3 upload: %w", err)
		}
		log.Info("auto-deploy: git bundle uploaded to S3")
	}

	// (Continues with cancel + submit below — those are push-flow-specific.)
	// Bundle-prep ends here. Preview flow calls PrepareSourceBundle directly.

	// Step 6: Cancel superseded QUEUED builds (MEET-044 P0).
	// When a newer push arrives, older QUEUED builds for the same environment
	// are cancelled. This ensures the LATEST push deploys, not the first.
	// Without this, rapid pushes deploy the older (possibly broken) commit.
	if d.cfg.BuildCanceller != nil {
		cancelled, cancelErr := d.cfg.BuildCanceller.CancelQueuedBuilds(ctx, tenantID, projectName, "production")
		if cancelErr != nil {
			log.Warn("auto-deploy: cancel queued builds failed (non-fatal)", "error", cancelErr)
		} else if cancelled > 0 {
			log.Info("auto-deploy: cancelled superseded builds", "count", cancelled)
		}
	}

	// Step 7: Submit build
	if d.cfg.BuildSubmitter != nil {
		if err := d.cfg.BuildSubmitter.SubmitBuildFromWebhook(ctx, WebhookBuildRequest{
			TenantID:       tenantID,
			ProjectName:    projectName,
			Environment:    "production",
			CommitSHA:      commitSHA,
			InstallationID: installationID,
			RepoFullName:   repoFullName,
			Branch:         branch,
			CommitMessage:  commitMessage,
			SenderLogin:    senderLogin,
			LoopZeroYaml:       loopzeroYaml,
		}); err != nil {
			return fmt.Errorf("auto-deploy: submit build: %w", err)
		}
		log.Info("auto-deploy: build submitted successfully")
	} else {
		log.Info("auto-deploy: build submitter not configured (webhook-only mode)")
	}

	// Step 6: Set commit status to pending.
	//
	// G118 expected: this is a fire-and-forget side-effect that runs
	// AFTER HandlePush returns. Inheriting the parent ctx would cancel
	// the commit-status update the moment the HTTP handler returns,
	// defeating the purpose. Use a separate 30s timeout so the
	// goroutine cannot run forever.
	if d.cfg.AppClient != nil {
		go func() { //nolint:gosec // G118 expected: detached fire-and-forget commit-status update
			statusCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := d.cfg.AppClient.SetCommitStatus(statusCtx, installationID, owner, repo, commitSHA, CommitStatus{
				State:       "pending",
				Description: "Deploying to production...",
				Context:     "loopzero/deploy",
			}); err != nil {
				log.Warn("auto-deploy: set commit status failed", "error", err)
			} else {
				log.Info("auto-deploy: commit status set to pending")
			}
		}()
	}

	return nil
}

// PrepareSourceBundle downloads the repo tarball at the given commit,
// converts it to a git bundle, and uploads to S3 under
// `<tenantID>/source/<commit>.bundle`. Idempotent: re-running for the
// same commit overwrites the bundle (safe — bundle is the same artifact).
//
// Shared between push auto-deploy (called inline by HandlePush) and
// PR preview deploy (called by PreviewDeployer before triggering
// CreatePreview). Without this, the build clone-repo step fails with
// "Object does not exist" trying to fetch the bundle from S3.
//
// LIVE catch 2026-06-07 J-23: PR webhook flow was triggering
// previewService.CreatePreview → SubmitBuild without first preparing
// the source bundle, so preview builds always failed at clone-repo.
//
// Bonus return: the parsed loopzero.yaml — preview deploy uses it to populate
// apps with framework/path/build so LanguageStrategy resolves the right
// build steps (bug #3 of the J-23 LIVE chain). Caller may pass nil for
// the LoopZeroYamlMinimal pointer when only the upload is needed.
func (d *AutoDeployer) PrepareSourceBundle(ctx context.Context, repoFullName, commitSHA string, installationID int64, tenantID string) (*LoopZeroYamlMinimal, error) {
	log := d.cfg.Logger.With(
		"repo", repoFullName,
		"sha", commitSHA,
		"tenant", tenantID,
	)

	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	log.Info("prepare-source-bundle: downloading tarball")
	tarballReader, err := d.cfg.AppClient.DownloadSource(ctx, installationID, owner, repo, commitSHA)
	if err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: download: %w", err)
	}
	defer func() { _ = tarballReader.Close() }()

	tmpFile, err := os.CreateTemp("", "loopzero-preview-source-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: tmp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	if _, err := io.Copy(tmpFile, tarballReader); err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: save tarball: %w", err)
	}

	// Parse loopzero.yaml from tarball (best-effort — preview build will fail
	// noisily later if it's missing, so we don't double-error here).
	if _, seekErr := tmpFile.Seek(0, io.SeekStart); seekErr != nil {
		return nil, fmt.Errorf("prepare-source-bundle: seek for parse: %w", seekErr)
	}
	loopzeroYaml, loopzeroYamlErr := extractLoopZeroYaml(tmpFile)
	if loopzeroYamlErr != nil {
		log.Warn("prepare-source-bundle: loopzero.yaml not found", "error", loopzeroYamlErr)
		// loopzeroYaml stays nil; caller treats as "no apps inherited from source".
	} else {
		log.Info("prepare-source-bundle: loopzero.yaml parsed", "apps", len(loopzeroYaml.Apps))
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: seek for convert: %w", err)
	}

	bundlePath, err := tarballToGitBundle(ctx, tmpFile, commitSHA, "preview build", "loopzero-preview-service")
	if err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: convert: %w", err)
	}
	defer func() { _ = os.Remove(bundlePath) }()

	if d.cfg.ObjectStore == nil {
		log.Warn("prepare-source-bundle: ObjectStore is nil — skipping upload (build will fail at clone-repo)")
		return loopzeroYaml, nil
	}
	bundleFile, err := os.Open(bundlePath) //nolint:gosec // tmpdir-internal
	if err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: open bundle: %w", err)
	}
	defer func() { _ = bundleFile.Close() }()
	objKey := fmt.Sprintf("%s/source/%s.bundle", tenantID, commitSHA)
	bucket := d.cfg.SourceBucket
	if bucket == "" {
		bucket = "loopzero-builds"
	}
	if err := d.cfg.ObjectStore.Upload(ctx, bucket, objKey, bundleFile); err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: upload: %w", err)
	}
	log.Info("prepare-source-bundle: uploaded", "bucket", bucket, "key", objKey)
	return loopzeroYaml, nil
}

// --- loopzero.yaml extraction ---

type LoopZeroYamlMinimal struct {
	Project string                       `yaml:"project"`
	Apps    map[string]map[string]interface{} `yaml:"apps"`
}

// tarballToGitBundle converts a GitHub tarball (.tar.gz) into a git bundle.
// The loopzero-clone CWT expects git bundle format, not raw tarballs.
// Process: safe extract tarball (StripComponents=1) → git init → git add → git commit → git bundle create.
//
// SEC: Uses archive.SafeExtractTarGz with symlink rejection, path traversal
// protection, and decompression size limits. Uses gitclient.GitRunner with
// context propagation for cancellation support.
func tarballToGitBundle(ctx context.Context, tarball io.Reader, commitSHA, commitMessage, author string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "loopzero-github-bundle-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	// Write tarball to disk for SafeExtractTarGz (needs seekable file).
	// G304 false positive: tarPath is constructed from os.MkdirTemp +
	// hardcoded suffix "source.tar.gz" — no user input on the path.
	tarPath := filepath.Join(tmpDir, "source.tar.gz")
	tarFile, err := os.Create(tarPath) //nolint:gosec // G304 false positive: tmpdir + literal filename
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("create tar file: %w", err)
	}
	if _, err := io.Copy(tarFile, tarball); err != nil {
		_ = tarFile.Close()
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("write tar file: %w", err)
	}
	_ = tarFile.Close()

	// SEC: Safe extraction with all protections + StripComponents=1
	// to remove the GitHub "owner-repo-sha/" prefix directory.
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("mkdir repo: %w", err)
	}
	if _, err := archive.SafeExtractTarGz(tarPath, repoDir, &archive.ExtractOpts{StripComponents: 1}); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("extract tarball: %w", err)
	}

	// CANONICAL FIX 2026-06-15 (FAANG SEM WORKAROUND): defense-in-depth
	// fallback when the upstream author identity is empty. Caught LIVE
	// dev-public connect-then-deploy 2026-06-15 — git rejected with
	// "fatal: empty ident name (for <@users.noreply.github.com>) not
	// allowed" because the GitHub HEAD commit author was empty (the
	// canonical-fixture repo's last commit had no author.login surfaced
	// by GetHeadCommit). The author param is purely informational at
	// bundle level (the deploy chain ultimately credits the real SHA);
	// substituting a canonical LoopZero author preserves the audit trail
	// without blocking the deploy. See repositories.go:461 + the LIVE
	// log line "connect-then-deploy: HandlePush failed: ... empty ident".
	authorName := author
	authorEmail := author + "@users.noreply.github.com"
	if authorName == "" {
		authorName = "LoopZero Platform"
		authorEmail = "noreply@loopzero.example"
	}
	// Create git repo and bundle using GitRunner (with context for cancellation).
	git := gitclient.NewGitRunner("", gitclient.WithEnv(
		"GIT_AUTHOR_NAME="+authorName,
		"GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME=LoopZero Platform",
		"GIT_COMMITTER_EMAIL=noreply@loopzero.example",
	))

	if err := git.Run(ctx, repoDir, "init"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("git init: %w", err)
	}
	if err := git.Run(ctx, repoDir, "add", "."); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("git add: %w", err)
	}

	msg := commitMessage
	if msg == "" {
		msg = "Auto-deploy from GitHub push"
	}
	if err := git.Run(ctx, repoDir, "commit", "-m", msg, "--allow-empty"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("git commit: %w", err)
	}

	shaPrefix := commitSHA
	if len(shaPrefix) > 12 {
		shaPrefix = shaPrefix[:12]
	}
	bundlePath := filepath.Join(os.TempDir(), fmt.Sprintf("loopzero-bundle-%s.bundle", shaPrefix))
	if err := git.Run(ctx, repoDir, "bundle", "create", bundlePath, "HEAD"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("git bundle create: %w", err)
	}

	_ = os.RemoveAll(tmpDir)
	return bundlePath, nil
}

// extractLoopZeroYaml reads loopzero.yaml from a GitHub tarball (.tar.gz).
// GitHub tarballs have a top-level directory like "owner-repo-sha/".
func extractLoopZeroYaml(r io.Reader) (*LoopZeroYamlMinimal, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}

		// GitHub tarball: files are under "owner-repo-sha/" prefix
		name := filepath.Base(header.Name)
		// Also check one level deep (the loopzero.yaml is at repo root)
		parts := strings.Split(header.Name, "/")
		if len(parts) == 2 && name == "loopzero.yaml" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read loopzero.yaml: %w", err)
			}
			var cfg LoopZeroYamlMinimal
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("parse loopzero.yaml: %w", err)
			}
			return &cfg, nil
		}
	}
	return nil, fmt.Errorf("loopzero.yaml not found in tarball")
}
