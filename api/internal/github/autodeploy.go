// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/archive"
	"github.com/getloopscale/loop-runtime/api/internal/gitclient"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
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
	SourceBucket   string // S3 bucket for source bundles (default: "loop-runtime-builds")
	Logger         *slog.Logger
	// BusyRetryEvery and BusyWait bound how a push waits for an environment
	// that is deploying: one submit attempt per BusyRetryEvery, for at most
	// BusyWait. Zero takes the defaults (15 seconds, 20 minutes).
	BusyRetryEvery time.Duration
	BusyWait       time.Duration
	// BranchHead reads the commit at the head of the pushed branch. Nil reads
	// it from GitHub through AppClient.
	BranchHead func(ctx context.Context, req PushRequest) (string, error)
	// BuildQuota gates a push on the plan's builds per month, the limit the
	// deploy route enforces. Nil builds every push.
	BuildQuota BuildQuota
}

const (
	defaultBusyRetryEvery = 15 * time.Second
	defaultBusyWait       = 20 * time.Minute
)

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
	TenantID        string
	ProjectName     string
	Environment     string
	CommitSHA       string
	InstallationID  int64
	RepoFullName    string
	Branch          string
	CommitMessage   string
	SenderLogin     string
	LoopRuntimeYaml *LoopRuntimeYamlMinimal
}

// AutoDeployer handles auto-deploy on push events.
type AutoDeployer struct {
	cfg AutoDeployConfig
}

// NewAutoDeployer creates a new AutoDeployer.
func NewAutoDeployer(cfg AutoDeployConfig) *AutoDeployer {
	return &AutoDeployer{cfg: cfg}
}

// PushRequest is one push to deploy: the repository and commit, the GitHub
// App installation that can read it, the LoopRuntime project it is linked to, and the
// commit metadata recorded on the build.
type PushRequest struct {
	RepoFullName   string
	Branch         string
	CommitSHA      string
	InstallationID int64
	TenantID       string
	ProjectName    string
	CommitMessage  string
	SenderLogin    string
}

const (
	// autoDeployEnvironment is the environment a push to the default branch
	// deploys to.
	autoDeployEnvironment = "production"
	defaultSourceBucket   = "loop-runtime-builds"
)

// HandlePush processes a push event and triggers a deploy if the repo is linked.
// This is called asynchronously from the webhook handler.
func (d *AutoDeployer) HandlePush(ctx context.Context, req PushRequest) error {
	ctx, span := otel.Tracer("loop-runtime-api").Start(ctx, "loop-runtime.api.github.autodeploy",
		trace.WithAttributes(
			attribute.String("loop-runtime.github.repo", req.RepoFullName),
			attribute.String("loop-runtime.github.commit_sha", req.CommitSHA),
		))
	defer span.End()

	log := d.cfg.Logger.With("repo", req.RepoFullName, "branch", req.Branch,
		"sha", req.CommitSHA, "project", req.ProjectName)
	log.Info("auto-deploy: starting")

	owner, repo, err := splitRepoFullName(req.RepoFullName)
	if err != nil {
		return err
	}
	refused, err := d.refusedByBuildQuota(ctx, log, req, owner, repo)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if refused {
		return nil
	}
	src, err := d.downloadSource(ctx, req.InstallationID, owner, repo, req.CommitSHA)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("auto-deploy: %w", err)
	}
	defer src.close()

	loopRuntimeYaml, err := src.deployDescriptor()
	if err != nil {
		// Not deployable by the same rule Connect applies: nothing to build.
		log.Warn("auto-deploy: repository is not deployable, skipping", "error", err)
		d.markCommitUndeployable(log, req, owner, repo, err)
		return nil
	}
	log.Info("auto-deploy: deploy descriptor found", "project_in_yaml", loopRuntimeYaml.Project, "apps", len(loopRuntimeYaml.Apps))

	if err := d.stageBundle(ctx, src, req.TenantID, req.CommitSHA, req.CommitMessage, req.SenderLogin); err != nil {
		return fmt.Errorf("auto-deploy: %w", err)
	}

	d.cancelSupersededBuilds(ctx, log, req)
	if err := d.submitBuild(ctx, log, req, loopRuntimeYaml); err != nil {
		return err
	}
	d.markCommitPending(log, req, owner, repo)
	return nil
}

// cancelSupersededBuilds cancels older QUEUED builds for the environment
// (MEET-044 P0): the LATEST push deploys, not the first. Best-effort.
func (d *AutoDeployer) cancelSupersededBuilds(ctx context.Context, log *slog.Logger, req PushRequest) {
	if d.cfg.BuildCanceller == nil {
		return
	}
	cancelled, err := d.cfg.BuildCanceller.CancelQueuedBuilds(ctx, req.TenantID, req.ProjectName, autoDeployEnvironment)
	if err != nil {
		log.Warn("auto-deploy: cancel queued builds failed (non-fatal)", "error", err)
	} else if cancelled > 0 {
		log.Info("auto-deploy: cancelled superseded builds", "count", cancelled)
	}
}

func (d *AutoDeployer) submitBuild(ctx context.Context, log *slog.Logger, req PushRequest, loopRuntimeYaml *LoopRuntimeYamlMinimal) error {
	if d.cfg.BuildSubmitter == nil {
		log.Info("auto-deploy: build submitter not configured (webhook-only mode)")
		return nil
	}
	build := WebhookBuildRequest{
		TenantID:        req.TenantID,
		ProjectName:     req.ProjectName,
		Environment:     autoDeployEnvironment,
		CommitSHA:       req.CommitSHA,
		InstallationID:  req.InstallationID,
		RepoFullName:    req.RepoFullName,
		Branch:          req.Branch,
		CommitMessage:   req.CommitMessage,
		SenderLogin:     req.SenderLogin,
		LoopRuntimeYaml: loopRuntimeYaml,
	}
	retryEvery, wait := d.cfg.BusyRetryEvery, d.cfg.BusyWait
	if retryEvery <= 0 {
		retryEvery = defaultBusyRetryEvery
	}
	if wait <= 0 {
		wait = defaultBusyWait
	}
	deadline := time.Now().Add(wait)

	// An environment takes one deploy at a time. A push that lands during one
	// was refused once and never built (#125); it now waits for the
	// environment, and gives way when the branch moved on, because the newer
	// push has a handler of its own and only the newest commit must be built.
	for {
		err := d.cfg.BuildSubmitter.SubmitBuildFromWebhook(ctx, build)
		if err == nil {
			log.Info("auto-deploy: build submitted successfully")
			return nil
		}
		if !errors.Is(err, loopruntimeerrors.ErrDeployInProgress) {
			return fmt.Errorf("auto-deploy: submit build: %w", err)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("auto-deploy: the environment was still deploying after %s, so commit %s was not built: %w", wait, req.CommitSHA, err)
		}
		log.Info("auto-deploy: the environment is deploying; the push waits", "retry_in", retryEvery.String())
		select {
		case <-ctx.Done():
			return fmt.Errorf("auto-deploy: stopped while commit %s waited for the environment: %w", req.CommitSHA, ctx.Err())
		case <-time.After(retryEvery):
		}
		head, headErr := d.branchHead(ctx, req)
		if headErr != nil {
			log.Warn("auto-deploy: could not read the branch head while waiting; the push keeps waiting", "error", headErr)
			continue
		}
		if head != req.CommitSHA {
			log.Info("auto-deploy: a newer commit was pushed while this one waited; skipping it", "head", head)
			return nil
		}
	}
}

func (d *AutoDeployer) branchHead(ctx context.Context, req PushRequest) (string, error) {
	if d.cfg.BranchHead != nil {
		return d.cfg.BranchHead(ctx, req)
	}
	owner, repo, err := splitRepoFullName(req.RepoFullName)
	if err != nil {
		return "", err
	}
	head, err := d.cfg.AppClient.GetHeadCommit(ctx, req.InstallationID, owner, repo, req.Branch)
	if err != nil {
		return "", err
	}
	return head.SHA, nil
}

// markCommitPending sets the commit status to pending. It is a fire-and-forget
// side-effect that outlives HandlePush: inheriting the caller's ctx would
// cancel the update the moment the HTTP handler returns, so it runs detached
// with its own 30s bound.
func (d *AutoDeployer) markCommitPending(log *slog.Logger, req PushRequest, owner, repo string) {
	if d.cfg.AppClient == nil {
		return
	}
	go func() { //nolint:gosec // G118 expected: detached fire-and-forget commit-status update
		statusCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := d.cfg.AppClient.SetCommitStatus(statusCtx, req.InstallationID, owner, repo, req.CommitSHA, CommitStatus{
			State:       "pending",
			Description: "Deploying to production...",
			Context:     "loop-runtime/deploy",
		}); err != nil {
			log.Warn("auto-deploy: set commit status failed", "error", err)
		} else {
			log.Info("auto-deploy: commit status set to pending")
		}
	}()
}

// markCommitUndeployable tells the person who pushed that nothing was built,
// and why. Without it the push leaves no trace on GitHub: no build exists to
// report on, so no later status would ever arrive.
func (d *AutoDeployer) markCommitUndeployable(log *slog.Logger, req PushRequest, owner, repo string, cause error) {
	if d.cfg.AppClient == nil {
		return
	}
	description := DeployDescriptorPath + " was not found at the repository root, so nothing was deployed."
	var invalid *InvalidDeployDescriptorError
	if errors.As(cause, &invalid) {
		description = invalid.Error()
	}
	// Detached for the reason markCommitPending gives; it waits, because
	// HandlePush returns right after and there is nothing else to run.
	statusCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.cfg.AppClient.SetCommitStatus(statusCtx, req.InstallationID, owner, repo, req.CommitSHA, CommitStatus{
		State:       "failure",
		Description: fitCommitStatusDescription(description),
		Context:     "loop-runtime/deploy",
	}); err != nil {
		log.Warn("auto-deploy: set commit status failed", "error", err)
		return
	}
	log.Info("auto-deploy: commit marked as failure, the repository is not deployable")
}

// PrepareSourceBundle downloads the repo tarball at the given commit,
// converts it to a git bundle, and uploads to S3 under
// `<tenantID>/source/<commit>.bundle`. Idempotent: re-running for the
// same commit overwrites the bundle (safe — bundle is the same artifact).
//
// Shared between push auto-deploy and PR preview deploy (called by
// PreviewDeployer before triggering CreatePreview). Without this, the build
// clone-repo step fails with "Object does not exist" trying to fetch the
// bundle from S3 (LIVE catch 2026-06-07 J-23).
//
// It also returns the deploy descriptor (ParseDeployDescriptor) — preview
// deploy uses it to populate apps with framework/path/build so
// LanguageStrategy resolves the right build steps. A repository with no
// deployable descriptor returns nil and still uploads: the preview then
// inherits the apps of the latest successful build.
func (d *AutoDeployer) PrepareSourceBundle(ctx context.Context, repoFullName, commitSHA string, installationID int64, tenantID string) (*LoopRuntimeYamlMinimal, error) {
	log := d.cfg.Logger.With("repo", repoFullName, "sha", commitSHA, "tenant", tenantID)

	owner, repo, err := splitRepoFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	log.Info("prepare-source-bundle: downloading tarball")
	src, err := d.downloadSource(ctx, installationID, owner, repo, commitSHA)
	if err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: %w", err)
	}
	defer src.close()

	loopRuntimeYaml, descriptorErr := src.deployDescriptor()
	if descriptorErr != nil {
		log.Warn("prepare-source-bundle: repository is not deployable; the preview falls back to the last build's apps", "error", descriptorErr)
		loopRuntimeYaml = nil
	} else {
		log.Info("prepare-source-bundle: deploy descriptor parsed", "apps", len(loopRuntimeYaml.Apps))
	}

	if err := d.stageBundle(ctx, src, tenantID, commitSHA, "preview build", "loop-runtime-preview-service"); err != nil {
		return nil, fmt.Errorf("prepare-source-bundle: %w", err)
	}
	return loopRuntimeYaml, nil
}

func splitRepoFullName(repoFullName string) (owner, repo string, err error) {
	owner, repo, ok := splitOwnerRepo(repoFullName)
	if !ok {
		return "", "", fmt.Errorf("invalid repo name: %s", repoFullName)
	}
	return owner, repo, nil
}

// sourceTarball is a downloaded GitHub tarball kept in a temp file, because
// it is read twice: once for the deploy descriptor, once for the bundle
// conversion.
type sourceTarball struct {
	file *os.File
}

func (d *AutoDeployer) downloadSource(ctx context.Context, installationID int64, owner, repo, commitSHA string) (*sourceTarball, error) {
	body, err := d.cfg.AppClient.DownloadSource(ctx, installationID, owner, repo, commitSHA)
	if err != nil {
		return nil, fmt.Errorf("download source: %w", err)
	}
	defer func() { _ = body.Close() }()

	f, err := os.CreateTemp("", "loop-runtime-github-source-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	src := &sourceTarball{file: f}
	if _, err := io.Copy(f, body); err != nil {
		src.close()
		return nil, fmt.Errorf("save tarball: %w", err)
	}
	return src, nil
}

func (s *sourceTarball) rewound() (io.Reader, error) {
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind tarball: %w", err)
	}
	return s.file, nil
}

func (s *sourceTarball) deployDescriptor() (*LoopRuntimeYamlMinimal, error) {
	r, err := s.rewound()
	if err != nil {
		return nil, err
	}
	return extractLoopRuntimeYaml(r)
}

func (s *sourceTarball) close() {
	_ = s.file.Close()
	_ = os.Remove(s.file.Name())
}

// stageBundle converts the tarball to a git bundle (the loop-runtime-clone step
// expects bundle format) and uploads it to `<tenantID>/source/<sha>.bundle`.
// With no object store configured the upload is skipped with a warning.
func (d *AutoDeployer) stageBundle(ctx context.Context, src *sourceTarball, tenantID, commitSHA, commitMessage, author string) error {
	r, err := src.rewound()
	if err != nil {
		return err
	}
	bundlePath, cleanup, err := tarballToGitBundle(ctx, r, commitSHA, commitMessage, author)
	if err != nil {
		return fmt.Errorf("convert to git bundle: %w", err)
	}
	defer cleanup()

	if d.cfg.ObjectStore == nil {
		d.cfg.Logger.Warn("source bundle: no object store configured — upload skipped (the build will fail at clone-repo)",
			"tenant", tenantID, "sha", commitSHA)
		return nil
	}
	bundle, err := os.Open(bundlePath) //nolint:gosec // G304 false positive: bundlePath is inside tarballToGitBundle's private temp dir
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer func() { _ = bundle.Close() }()

	bucket := d.cfg.SourceBucket
	if bucket == "" {
		bucket = defaultSourceBucket
	}
	key := fmt.Sprintf("%s/source/%s.bundle", tenantID, commitSHA)
	if err := d.cfg.ObjectStore.Upload(ctx, bucket, key, bundle); err != nil {
		return fmt.Errorf("S3 upload: %w", err)
	}
	d.cfg.Logger.Info("source bundle uploaded", "bucket", bucket, "key", key)
	return nil
}

// --- loopzero.yaml extraction ---

type LoopRuntimeYamlMinimal struct {
	Project string                            `yaml:"project"`
	Apps    map[string]map[string]interface{} `yaml:"apps"`
}

// tarballToGitBundle converts a GitHub tarball (.tar.gz) into a git bundle.
// The loop-runtime-clone CWT expects git bundle format, not raw tarballs.
// Process: safe extract tarball (StripComponents=1) → git init → git add → git commit → git bundle create.
//
// SEC: Uses archive.SafeExtractTarGz with symlink rejection, path traversal
// protection, and decompression size limits. Uses gitclient.GitRunner with
// context propagation for cancellation support.
//
// The bundle is written inside the conversion's private temp dir and the
// returned cleanup removes that dir: two conversions of the same commit (a
// push and a PR preview, or two tenants deploying one upstream commit) must
// never share, and delete, one another's bundle.
func tarballToGitBundle(ctx context.Context, tarball io.Reader, commitSHA, commitMessage, author string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "loop-runtime-github-bundle-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	// Write tarball to disk for SafeExtractTarGz (needs seekable file).
	// G304 false positive: tarPath is constructed from os.MkdirTemp +
	// hardcoded suffix "source.tar.gz" — no user input on the path.
	tarPath := filepath.Join(tmpDir, "source.tar.gz")
	tarFile, err := os.Create(tarPath) //nolint:gosec // G304 false positive: tmpdir + literal filename
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create tar file: %w", err)
	}
	if _, err := io.Copy(tarFile, tarball); err != nil {
		_ = tarFile.Close()
		cleanup()
		return "", nil, fmt.Errorf("write tar file: %w", err)
	}
	_ = tarFile.Close()

	// SEC: Safe extraction with all protections + StripComponents=1
	// to remove the GitHub "owner-repo-sha/" prefix directory.
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("mkdir repo: %w", err)
	}
	if _, err := archive.SafeExtractTarGz(tarPath, repoDir, &archive.ExtractOpts{StripComponents: 1}); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extract tarball: %w", err)
	}

	// CANONICAL FIX 2026-06-15 (FAANG SEM WORKAROUND): defense-in-depth
	// fallback when the upstream author identity is empty. Caught LIVE
	// dev-public connect-then-deploy 2026-06-15 — git rejected with
	// "fatal: empty ident name (for <@users.noreply.github.com>) not
	// allowed" because the GitHub HEAD commit author was empty (the
	// canonical-fixture repo's last commit had no author.login surfaced
	// by GetHeadCommit). The author param is purely informational at
	// bundle level (the deploy chain ultimately credits the real SHA);
	// substituting a canonical LoopRuntime author preserves the audit trail
	// without blocking the deploy. See repositories.go:461 + the LIVE
	// log line "connect-then-deploy: HandlePush failed: ... empty ident".
	authorName := author
	authorEmail := author + "@users.noreply.github.com"
	if authorName == "" {
		authorName = "LoopRuntime Platform"
		authorEmail = "noreply@getloopscale.com"
	}
	// Create git repo and bundle using GitRunner (with context for cancellation).
	git := gitclient.NewGitRunner("", gitclient.WithEnv(
		"GIT_AUTHOR_NAME="+authorName,
		"GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME=LoopRuntime Platform",
		"GIT_COMMITTER_EMAIL=noreply@getloopscale.com",
	))

	if err := git.Run(ctx, repoDir, "init"); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("git init: %w", err)
	}
	if err := git.Run(ctx, repoDir, "add", "."); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("git add: %w", err)
	}

	msg := commitMessage
	if msg == "" {
		msg = "Auto-deploy from GitHub push"
	}
	if err := git.Run(ctx, repoDir, "commit", "-m", msg, "--allow-empty"); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("git commit: %w", err)
	}

	bundlePath := filepath.Join(tmpDir, "source.bundle")
	if err := git.Run(ctx, repoDir, "bundle", "create", bundlePath, "HEAD"); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("git bundle create: %w", err)
	}
	return bundlePath, cleanup, nil
}

// extractLoopRuntimeYaml reads the deploy descriptor from a GitHub tarball (.tar.gz)
// and applies ParseDeployDescriptor to it. GitHub tarballs have a top-level
// directory like "owner-repo-sha/".
func extractLoopRuntimeYaml(r io.Reader) (*LoopRuntimeYamlMinimal, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	// A repository may carry more than one of the names; which one is read must
	// not depend on the order of the archive.
	found := make(map[string][]byte, len(DeployDescriptorPaths))
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}

		// GitHub tarball: the repository root is the "owner-repo-sha/" directory.
		parts := strings.Split(header.Name, "/")
		if len(parts) != 2 || !slices.Contains(DeployDescriptorPaths, parts[1]) {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", parts[1], err)
		}
		found[parts[1]] = data
	}
	for _, path := range DeployDescriptorPaths {
		if data, ok := found[path]; ok {
			return ParseDeployDescriptorFile(path, data)
		}
	}
	return nil, fmt.Errorf("%s not found in tarball", DeployDescriptorPath)
}
