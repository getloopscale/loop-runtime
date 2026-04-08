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
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AutoDeployConfig holds configuration for auto-deploy on push.
type AutoDeployConfig struct {
	AppClient    *AppClient
	SourceStore  SourcePusher
	ObjectStore  ObjectUploader
	BuildSubmitter BuildSubmitter
	Logger       *slog.Logger
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
	LoopZeroYaml       *loopzeroYamlMinimal
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
		return fmt.Errorf("auto-deploy: download source: %w", err)
	}
	defer tarballReader.Close()

	// Step 2: Save tarball to temp file (need to read twice: parse loopzero.yaml + upload)
	tmpFile, err := os.CreateTemp("", "loopzero-github-source-*.tar.gz")
	if err != nil {
		return fmt.Errorf("auto-deploy: create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

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
	bundlePath, err := tarballToGitBundle(tmpFile, commitSHA, commitMessage, senderLogin)
	if err != nil {
		return fmt.Errorf("auto-deploy: convert to git bundle: %w", err)
	}
	defer os.Remove(bundlePath)
	log.Info("auto-deploy: tarball converted to git bundle")

	// Step 5: Upload git bundle to S3 for build cluster
	if d.cfg.ObjectStore != nil {
		bundleFile, err := os.Open(bundlePath)
		if err != nil {
			return fmt.Errorf("auto-deploy: open bundle: %w", err)
		}
		defer bundleFile.Close()
		objKey := fmt.Sprintf("%s/source/%s.bundle", tenantID, commitSHA)
		if err := d.cfg.ObjectStore.Upload(ctx, "loopzero-builds", objKey, bundleFile); err != nil {
			return fmt.Errorf("auto-deploy: S3 upload: %w", err)
		}
		log.Info("auto-deploy: git bundle uploaded to S3")
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

	// Step 6: Set commit status to pending
	if d.cfg.AppClient != nil {
		go func() {
			statusCtx := context.Background()
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

// --- loopzero.yaml extraction ---

type loopzeroYamlMinimal struct {
	Project string                       `yaml:"project"`
	Apps    map[string]map[string]interface{} `yaml:"apps"`
}

// tarballToGitBundle converts a GitHub tarball (.tar.gz) into a git bundle.
// The loopzero-clone CWT expects git bundle format, not raw tarballs.
// Process: extract tarball → git init → git add → git commit → git bundle create.
func tarballToGitBundle(tarball io.Reader, commitSHA, commitMessage, author string) (string, error) {
	// Create temp dir for extraction
	tmpDir, err := os.MkdirTemp("", "loopzero-github-bundle-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	// Extract tarball
	gz, err := gzip.NewReader(tarball)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var topLevelDir string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("tar read: %w", err)
		}

		// GitHub tarballs have a top-level dir like "owner-repo-sha/"
		// Strip it so files are at root of tmpDir
		parts := strings.SplitN(header.Name, "/", 2)
		if topLevelDir == "" && len(parts) > 0 {
			topLevelDir = parts[0]
		}
		var relPath string
		if len(parts) > 1 {
			relPath = parts[1]
		} else {
			continue
		}
		if relPath == "" {
			continue
		}

		target := filepath.Join(tmpDir, relPath)

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0755)
			f, err := os.Create(target)
			if err != nil {
				continue
			}
			io.Copy(f, tr)
			f.Close()
			os.Chmod(target, os.FileMode(header.Mode))
		}
	}

	// Create git repo and bundle
	run := func(args ...string) error {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME="+author,
			"GIT_AUTHOR_EMAIL="+author+"@users.noreply.github.com",
			"GIT_COMMITTER_NAME=LoopZero Platform",
			"GIT_COMMITTER_EMAIL=noreply@loopzero.example",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %s: %w", args[0], string(out), err)
		}
		return nil
	}

	if err := run("git", "init"); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}
	if err := run("git", "add", "."); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}
	msg := commitMessage
	if msg == "" {
		msg = "Auto-deploy from GitHub push"
	}
	if err := run("git", "commit", "-m", msg, "--allow-empty"); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	bundlePath := filepath.Join(os.TempDir(), fmt.Sprintf("loopzero-bundle-%s.bundle", commitSHA[:12]))
	if err := run("git", "bundle", "create", bundlePath, "HEAD"); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	os.RemoveAll(tmpDir) // cleanup extracted files
	return bundlePath, nil
}

// extractLoopZeroYaml reads loopzero.yaml from a GitHub tarball (.tar.gz).
// GitHub tarballs have a top-level directory like "owner-repo-sha/".
func extractLoopZeroYaml(r io.Reader) (*loopzeroYamlMinimal, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

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
			var cfg loopzeroYamlMinimal
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("parse loopzero.yaml: %w", err)
			}
			return &cfg, nil
		}
	}
	return nil, fmt.Errorf("loopzero.yaml not found in tarball")
}
