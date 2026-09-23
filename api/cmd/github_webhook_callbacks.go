// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/github"
	ghwebhook "github.com/getloopscale/loopzero/api/internal/github/webhook"
	"github.com/getloopscale/loopzero/api/internal/services/preview"
)

// linkedRepoLookup finds the project a GitHub repository is linked to.
type linkedRepoLookup interface {
	GetByRepo(ctx context.Context, repoFullName string) (*store.ProjectRepository, error)
}

// pushDeployer starts an auto-deploy for one push.
type pushDeployer interface {
	HandlePush(ctx context.Context, req github.PushRequest) error
}

// installationRegistry records GitHub App installations.
type installationRegistry interface {
	SaveInstallation(ctx context.Context, id int64, accountLogin, accountType string, tenantID *string) error
	DeleteInstallation(ctx context.Context, installationID int64) error
}

// newPushHandler returns the push-event callback. Tag and delete pushes, pushes
// to a non-default branch, and repositories that are not linked or have
// auto-deploy off are ignored deliberately and return nil. A failed repository
// lookup is NOT a deliberate ignore: it returns the error, so the delivery is
// recorded as failed rather than as handled.
func newPushHandler(shutdownCtx context.Context, repos linkedRepoLookup, deployer pushDeployer, logger *slog.Logger) func(ghwebhook.PushEvent) error {
	return func(event ghwebhook.PushEvent) error {
		log := logger.With(
			"repo", event.Repository.FullName,
			"branch", event.Branch(),
			"sha", event.After,
			"sender", event.Sender.Login,
			"installation_id", event.Installation.ID,
		)
		if event.IsTag() || event.Deleted {
			log.Debug("github webhook: ignoring tag/delete push")
			return nil
		}
		if !event.IsDefaultBranch() {
			log.Info("github webhook: push to non-default branch (preview env not yet supported)")
			return nil
		}

		linked, err := repos.GetByRepo(shutdownCtx, event.Repository.FullName)
		if err != nil {
			return fmt.Errorf("github webhook: look up the project linked to %s: %w", event.Repository.FullName, err)
		}
		if linked == nil {
			log.Debug("github webhook: repo not linked to any project, ignoring")
			return nil
		}
		if !linked.AutoDeploy {
			log.Info("github webhook: auto-deploy disabled for this project")
			return nil
		}

		log = log.With("project", linked.ProjectName, "tenant", linked.TenantID)
		log.Info("github webhook: auto-deploy triggered")
		commitMsg := ""
		if event.HeadCommit != nil {
			commitMsg = event.HeadCommit.Message
		}
		go func() {
			// Cancelled on server shutdown; bounded so a slow GitHub/S3 call
			// cannot hang forever.
			deployCtx, cancel := context.WithTimeout(shutdownCtx, 5*time.Minute)
			defer cancel()
			if err := deployer.HandlePush(deployCtx, github.PushRequest{
				RepoFullName: event.Repository.FullName, Branch: event.Branch(), CommitSHA: event.After,
				InstallationID: event.Installation.ID, TenantID: linked.TenantID, ProjectName: linked.ProjectName,
				CommitMessage: commitMsg, SenderLogin: event.Sender.Login,
			}); err != nil {
				log.Error("github webhook: auto-deploy failed", "error", err)
			}
		}()
		return nil
	}
}

// newInstallationHandler returns the installation-event callback. A failure to
// record or remove the installation is returned, never logged-and-acknowledged:
// a stale installation row is otherwise permanent and invisible.
func newInstallationHandler(ctx context.Context, installs installationRegistry, logger *slog.Logger) func(ghwebhook.InstallationEvent) error {
	return func(event ghwebhook.InstallationEvent) error {
		logger.Info("github webhook: installation event",
			"action", event.Action,
			"installation_id", event.Installation.ID,
			"account", event.Installation.Account.Login,
		)
		switch event.Action {
		case "created":
			if err := installs.SaveInstallation(ctx,
				event.Installation.ID, event.Installation.Account.Login,
				event.Installation.Account.Type, nil); err != nil {
				return fmt.Errorf("github webhook: save installation %d: %w", event.Installation.ID, err)
			}
		case "deleted":
			if err := installs.DeleteInstallation(ctx, event.Installation.ID); err != nil {
				return fmt.Errorf("github webhook: delete installation %d: %w", event.Installation.ID, err)
			}
		}
		return nil
	}
}

// newPreviewLifecycle builds the preview service behind the PR webhook. It
// returns the service as the interface the deployer consumes, and a nil
// interface — not a typed nil — when construction fails, so the caller's nil
// check means what it says.
func newPreviewLifecycle(deps preview.PreviewServiceDeps) (github.PreviewLifecycleService, error) {
	svc, err := preview.NewPreviewService(deps)
	if err != nil {
		return nil, err
	}
	return svc, nil
}
