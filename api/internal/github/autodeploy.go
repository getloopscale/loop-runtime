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

	"gopkg.in/yaml.v3"
)

// AutoDeployConfig holds configuration for auto-deploy on push.
type AutoDeployConfig struct {
	AppClient    *AppClient
	SourceStore  SourceStore
	ObjectStore  ObjectUploader
	BuildSubmitter BuildSubmitter
	Logger       *slog.Logger
}

// SourceStore pushes source bundles (DIP — same as source.SourceStore).
type SourceStore interface {
	PushTarball(ctx context.Context, tenantID, projectName string, tarball io.Reader) (commitSHA string, err error)
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

	// Step 4: Upload to MinIO/Spaces for build cluster access
	if d.cfg.ObjectStore != nil {
		if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("auto-deploy: seek for upload: %w", err)
		}
		objKey := fmt.Sprintf("%s/source/%s.tar.gz", tenantID, commitSHA)
		if err := d.cfg.ObjectStore.Upload(ctx, "loopzero-builds", objKey, tmpFile); err != nil {
			log.Warn("auto-deploy: S3 upload failed (non-fatal in dev)", "error", err)
		}
	}

	// Step 5: Submit build
	if d.cfg.BuildSubmitter != nil {
		if err := d.cfg.BuildSubmitter.SubmitBuildFromWebhook(ctx, WebhookBuildRequest{
			TenantID:       tenantID,
			ProjectName:    projectName,
			Environment:    "production", // push to default branch = production
			CommitSHA:      commitSHA,
			InstallationID: installationID,
			RepoFullName:   repoFullName,
			Branch:         branch,
			CommitMessage:  commitMessage,
			SenderLogin:    senderLogin,
		}); err != nil {
			return fmt.Errorf("auto-deploy: submit build: %w", err)
		}
		log.Info("auto-deploy: build submitted")
	} else {
		log.Info("auto-deploy: build submitter not configured (logging only)")
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
