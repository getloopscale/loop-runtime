// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/getloopscale/loopzero/api/internal/adapters"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/getloopscale/loopzero/api/internal/preview"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/github"
	ghwebhook "github.com/getloopscale/loopzero/api/internal/github/webhook"
	"github.com/getloopscale/loopzero/api/internal/integrations/integrationshttp"
	"github.com/getloopscale/loopzero/api/internal/repositories/repositorieshttp"
)

// GitHubDeps holds GitHub App integration components.
type GitHubDeps struct {
	WebhookHandler http.Handler
	RepoHandler    *repositorieshttp.RepositoryHandler
	AppClient      *github.AppClient
	// Plan J-09 T2.1 (2026-06-05): dashboard repo picker source.
	// Nil when the GitHub App client is not configured -- router guards.
	IntegrationsHandler *integrationshttp.IntegrationsGitHubHandler
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
	orch *build.BuildOrchestrator,
	k8sDeps *K8sDeps,
	cfg config.Config,
	shutdownCtx context.Context,
	logger *slog.Logger,
) *GitHubDeps {
	// Repository store for webhook lookups and CRUD. Required for the
	// RepositoryHandler — always created when the DB is reachable.
	repoStore := &store.PostgresRepositoryStore{DB: dbAdapter}
	repoHandler := repositorieshttp.NewRepositoryHandler(&adapters.RepoStoreAdapter{Store: repoStore})

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
		ghAppClient, clientErr = github.NewAppClient(appID, appPrivateKey, cfg.GitHubAPIBaseURL)
		if clientErr != nil {
			logger.Error("github app client creation failed", "error", clientErr)
		} else if ghAppClient != nil {
			logger.Info("github app client created", "app_id", appID)
		}
	}

	// Register commit status callbacks if GitHub App client is available —
	// these are orthogonal to the webhook handler: they push statuses when
	// builds succeed/fail, independent of incoming webhook delivery.
	//
	// Plan J-09 T2.1 (2026-06-05): also wire the IntegrationsGitHubHandler
	// for the dashboard repo picker. Lives behind the ghAppClient nil
	// guard because there is no way to list installation repos without an
	// installation-token mint.
	var integrationsHandler *integrationshttp.IntegrationsGitHubHandler
	if ghAppClient != nil {
		statusNotifier := github.NewStatusNotifier(ghAppClient, &adapters.RepoLookupAdapter{Store: repoStore}, &adapters.BuildLookupAdapter{Store: stores.BuildStore}, cfg.BaseDomain, logger)
		if statusNotifier != nil {
			release.RegisterReleaseActivatedCallback(statusNotifier.OnBuildSucceeded)
			release.RegisterBuildFailedCallback(statusNotifier.OnBuildFailed)
			logger.Info("github: commit status callbacks registered (global)")
		}
		integrationsHandler = integrationshttp.NewIntegrationsGitHubHandler(
			&adapters.InstallationRepoListerAdapter{Client: ghAppClient},
			&adapters.InstallationLookupAdapter{Store: repoStore},
		)
		logger.Info("github: integrations handler registered", "endpoint", "/api/v1/integrations/github/installations/{id}/repositories")
	}

	// Auto-deployer — handles push → download source → upload S3 → submit build.
	// HOISTED 2026-06-09 (connect-then-deploy fix): created BEFORE the webhook
	// secret guard so the RepositoryHandler can also use it for Vercel-canonical
	// connect-time HEAD deploy. Without the hoist, autoDeployer existed only
	// inside the webhook-secret-set branch and Connect had no way to dispatch
	// a build on link → user saw empty deployments UI after connecting
	// (sponsor walkthrough catch 2026-06-09).
	var buildSub github.BuildSubmitter
	if orch != nil {
		buildSub = &adapters.BuildSubmitterAdapter{
			Orchestrator: orch,
			Logger:       logger,
		}
	}
	var autoDeployer *github.AutoDeployer
	if ghAppClient != nil && buildSub != nil {
		autoDeployer = github.NewAutoDeployer(github.AutoDeployConfig{
			AppClient:      ghAppClient,
			ObjectStore:    stores.ObjUploader,
			BuildSubmitter: buildSub,
			Logger:         logger,
		})
		// Wire connect-then-deploy onto the RepositoryHandler so POST
		// /api/v1/projects/{name}/connect dispatches an immediate HEAD
		// deploy after a successful link.
		repoHandler = repoHandler.WithAutoDeployer(autoDeployer, shutdownCtx)
	}

	// Wire the GitHub App client as the RepositoryHandler's introspector
	// (loopzero.yaml validation + GetHeadCommit for connect-then-deploy). The
	// WithIntrospector method existed since Plan J-09 T5.1 but was never
	// hooked in production wiring — caught 2026-06-09 sponsor walkthrough
	// while wiring WithAutoDeployer (both now mandatory for Vercel-canonical
	// connect UX).
	if ghAppClient != nil {
		repoHandler = repoHandler.WithIntrospector(ghAppClient)
	}

	// Webhook handler — requires the HMAC secret to validate inbound deliveries.
	// Without it, WebhookHandler stays nil and the /webhooks/github route is not
	// registered (router guards with `if cfg.GitHubWebhookHandler != nil`).
	githubWebhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if githubWebhookSecret == "" {
		logger.Info("github webhook: GITHUB_WEBHOOK_SECRET not set, webhook disabled (RepositoryHandler still active)")
		return &GitHubDeps{
			WebhookHandler:      nil,
			RepoHandler:         repoHandler,
			AppClient:           ghAppClient,
			IntegrationsHandler: integrationsHandler,
		}
	}

	if autoDeployer == nil {
		// Defensive: webhook handler below requires autoDeployer. If we got
		// here with webhook-secret set but autoDeployer nil, it means
		// ghAppClient OR buildSub were missing — log + skip webhook setup
		// rather than nil-deref the goroutine.
		logger.Warn("github webhook: autoDeployer unavailable (missing AppClient or BuildSubmitter) — webhook stays disabled")
		return &GitHubDeps{
			WebhookHandler:      nil,
			RepoHandler:         repoHandler,
			AppClient:           ghAppClient,
			IntegrationsHandler: integrationsHandler,
		}
	}

	wh := ghwebhook.NewHandler(shutdownCtx, githubWebhookSecret, logger)
	wh.OnPush(newPushHandler(shutdownCtx, repoStore, autoDeployer, logger))
	// Sprint 5 / T3.1 — wire PreviewDeployer.HandlePR. The deployer dispatches
	// by event.Action: opened/synchronize/reopened → CreatePreview;
	// closed → DeletePreviewByPR. Auto-deploy guard enforced inside.
	previewStore := store.NewPostgresPreviewStore(dbAdapter, dbAdapter)
	previewSvc, psErr := newPreviewLifecycle(preview.PreviewServiceDeps{
		Store:     previewStore,
		Builds:    orch,
		BuildApps: &adapters.PreviewAppsLookupAdapter{Builds: stores.BuildStore},
		// LIVE catch 2026-06-07 J-23 bug #4: PreviewCRCreator interface
		// had no production wiring. Canonical manager lives in
		// internal/k8s next to TenantCRManager, sharing the namespace
		// constant + dynamic client.
		CRCreator:           &adapters.PreviewCRCreatorAdapter{Mgr: k8sDeps.PreviewCRMgr},
		BaseDomain:          cfg.BaseDomain,
		PreviewPublicDomain: cfg.PreviewPublicDomain,
		Logger:              logger,
		// 2026-06-07 D2/D3/D6: single-slot SOT + env_target preview kind.
		SlotStore:  stores.PreviewSlotStore,
		EnvTargets: stores.EnvTargetStore,
	})
	if psErr != nil {
		// No PR handler is registered: a preview deployer over a service that
		// does not exist would accept the delivery and fail on the first call.
		logger.Error("preview service init failed — PR webhooks disabled", "error", psErr)
	} else {
		previewDeployer := github.NewPreviewDeployer(previewSvc, &adapters.ConnectedRepoAdapter{Store: repoStore}, logger).
			WithBundlePreparer(autoDeployer) // shares the same bundle-prep logic
		wh.OnPullRequest(previewDeployer.HandlePR)
	}
	wh.OnInstallation(newInstallationHandler(repoStore, logger))

	logger.Info("github webhook: handler registered", "endpoint", "/api/v1/webhooks/github")

	return &GitHubDeps{
		WebhookHandler:      wh,
		RepoHandler:         repoHandler,
		AppClient:           ghAppClient,
		IntegrationsHandler: integrationsHandler,
	}
}
