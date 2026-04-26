// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/github"
	ghwebhook "github.com/loopzero/loopzero/api/internal/github/webhook"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
)

// ErrPreviewNotImplemented is returned by the GitHub PR webhook handler
// while preview environments (Sprint 5 / T2.1 / COMP-001) are still in
// progress. Closes COMP-008 trust gap by making the silent no-op visible
// in logs and assertable in tests.
var ErrPreviewNotImplemented = errors.New("preview environments not yet implemented")

// GitHubDeps holds GitHub App integration components.
type GitHubDeps struct {
	WebhookHandler http.Handler
	RepoHandler    *routes.RepositoryHandler
	AppClient      *github.AppClient
}

// setupGitHub configures the GitHub App webhook handler, auto-deployer,
// commit status callbacks, and repository handler.
//
// CHG-2026-04-23-AUDIT-20: returns a non-nil GitHubDeps whenever dbAdapter
// is present. The RepositoryHandler (connect/disconnect CRUD) does not
// depend on GITHUB_WEBHOOK_SECRET or the GitHub App client — it only needs
// the repository store. The webhook handler is created only if the secret
// is set; without it, WebhookHandler is left nil (router already guards
// with `if cfg.GitHubWebhookHandler != nil`). This restores Rule 10
// ("Local equals production") for the connect/disconnect flow.
func setupGitHub(
	dbAdapter *db.PgxAdapter,
	stores *StoreDeps,
	orch *services.BuildOrchestrator,
	cfg config.Config,
	shutdownCtx context.Context,
	logger *slog.Logger,
) *GitHubDeps {
	// Repository store for webhook lookups and CRUD. Required for the
	// RepositoryHandler — always created when the DB is reachable.
	repoStore := &db.PostgresRepositoryStore{DB: dbAdapter}
	repoHandler := routes.NewRepositoryHandler(&repoStoreAdapter{store: repoStore})

	// GitHub App client for API calls (source download, commit status, PR comments).
	// Optional — only needed when GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY are set.
	var ghAppClient *github.AppClient
	appIDStr := os.Getenv("GITHUB_APP_ID")
	appPrivateKey := os.Getenv("GITHUB_APP_PRIVATE_KEY")
	if appIDStr != "" && appPrivateKey != "" {
		appID, parseErr := strconv.ParseInt(appIDStr, 10, 64)
		if parseErr != nil {
			logger.Error("GITHUB_APP_ID is set but invalid", "value", appIDStr, "error", parseErr)
			os.Exit(1)
		}
		var clientErr error
		ghAppClient, clientErr = github.NewAppClient(appID, appPrivateKey)
		if clientErr != nil {
			logger.Error("github app client creation failed", "error", clientErr)
		} else if ghAppClient != nil {
			logger.Info("github app client created", "app_id", appID)
		}
	}

	// Register commit status callbacks if GitHub App client is available —
	// these are orthogonal to the webhook handler: they push statuses when
	// builds succeed/fail, independent of incoming webhook delivery.
	if ghAppClient != nil {
		statusNotifier := github.NewStatusNotifier(ghAppClient, &repoLookupAdapter{store: repoStore}, &buildLookupAdapter{store: stores.BuildStore}, cfg.BaseDomain, logger)
		if statusNotifier != nil {
			services.RegisterReleaseActivatedCallback(statusNotifier.OnBuildSucceeded)
			services.RegisterBuildFailedCallback(statusNotifier.OnBuildFailed)
			logger.Info("github: commit status callbacks registered (global)")
		}
	}

	// Webhook handler — requires the HMAC secret to validate inbound deliveries.
	// Without it, WebhookHandler stays nil and the /webhooks/github route is not
	// registered (router guards with `if cfg.GitHubWebhookHandler != nil`).
	githubWebhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if githubWebhookSecret == "" {
		logger.Info("github webhook: GITHUB_WEBHOOK_SECRET not set, webhook disabled (RepositoryHandler still active)")
		return &GitHubDeps{
			WebhookHandler: nil,
			RepoHandler:    repoHandler,
			AppClient:      ghAppClient,
		}
	}

	// Auto-deployer — handles push → download source → upload S3 → submit build
	var buildSub github.BuildSubmitter
	if orch != nil {
		buildSub = &buildSubmitterAdapter{
			orchestrator: orch,
			logger:       logger,
		}
	}
	autoDeployer := github.NewAutoDeployer(github.AutoDeployConfig{
		AppClient:      ghAppClient,
		ObjectStore:    stores.ObjUploader,
		BuildSubmitter: buildSub,
		Logger:         logger,
	})

	wh := ghwebhook.NewHandler(githubWebhookSecret, logger)
	wh.OnPush(func(event ghwebhook.PushEvent) error {
		log := logger.With(
			"repo", event.Repository.FullName,
			"branch", event.Branch(),
			"sha", event.After,
			"sender", event.Sender.Login,
			"installation_id", event.Installation.ID,
		)

		// Ignore non-branch pushes (tags, deletes)
		if event.IsTag() || event.Deleted {
			log.Debug("github webhook: ignoring tag/delete push")
			return nil
		}

		// Only auto-deploy pushes to default branch (production)
		if !event.IsDefaultBranch() {
			log.Info("github webhook: push to non-default branch (preview env not yet supported)")
			return nil
		}

		// Look up linked project
		linked, err := repoStore.GetByRepo(context.Background(), event.Repository.FullName)
		if err != nil {
			log.Error("github webhook: repo lookup failed", "error", err)
			return nil // don't fail the webhook
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

		// Trigger auto-deploy (async — don't block webhook response)
		commitMsg := ""
		if event.HeadCommit != nil {
			commitMsg = event.HeadCommit.Message
		}
		go func() {
			// Use shutdownCtx-derived context so auto-deploy is cancelled on server shutdown.
			// 5-minute timeout prevents indefinite hangs on slow GitHub/S3 operations.
			deployCtx, cancel := context.WithTimeout(shutdownCtx, 5*time.Minute)
			defer cancel()
			if err := autoDeployer.HandlePush(deployCtx,
				event.Repository.FullName, event.Branch(), event.After,
				event.Installation.ID, linked.TenantID, linked.ProjectName,
				commitMsg, event.Sender.Login,
			); err != nil {
				log.Error("github webhook: auto-deploy failed", "error", err)
			}
		}()

		return nil
	})
	wh.OnPullRequest(func(event ghwebhook.PullRequestEvent) error {
		// COMP-008 / T2.2: previously this handler returned nil with a TODO,
		// silently acknowledging PR webhooks while no preview environment was
		// created. That created a trust gap (TC-2): users believed PRs deployed.
		// Until Sprint 5 lands, log an explicit WARN and return a sentinel error
		// so the no-op is visible in Loki and assertable in tests.
		logger.Warn("github webhook: pull_request received but preview environments not yet implemented (Sprint 5 in progress)",
			"repo", event.Repository.FullName,
			"action", event.Action,
			"pr", event.Number,
			"branch", event.PullRequest.Head.Ref,
			"feature", "preview-environments",
			"status", "not-implemented",
			"plan_ref", "docs/plans/operators-deep-review-remediation-plan.md#T2.1",
		)
		return ErrPreviewNotImplemented
	})
	wh.OnInstallation(func(event ghwebhook.InstallationEvent) error {
		logger.Info("github webhook: installation event",
			"action", event.Action,
			"installation_id", event.Installation.ID,
			"account", event.Installation.Account.Login,
		)
		// Save/remove installation in DB
		switch event.Action {
		case "created":
			if err := repoStore.SaveInstallation(context.Background(),
				event.Installation.ID, event.Installation.Account.Login,
				event.Installation.Account.Type, nil); err != nil {
				logger.Error("github webhook: failed to save installation",
					"installation_id", event.Installation.ID,
					"action", event.Action, "error", err)
			}
		case "deleted":
			if err := repoStore.DeleteInstallation(context.Background(), event.Installation.ID); err != nil {
				logger.Error("github webhook: failed to delete installation",
					"installation_id", event.Installation.ID,
					"action", event.Action, "error", err)
			}
		}
		return nil
	})

	logger.Info("github webhook: handler registered", "endpoint", "/api/v1/webhooks/github")

	return &GitHubDeps{
		WebhookHandler: wh,
		RepoHandler:    repoHandler,
		AppClient:      ghAppClient,
	}
}

