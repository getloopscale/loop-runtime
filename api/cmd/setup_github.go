// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/getloopscale/loop-runtime/api/internal/builds/buildsadapter"
	"github.com/getloopscale/loop-runtime/api/internal/preview/previewadapter"

	"github.com/getloopscale/loop-runtime/api/internal/builds"

	"github.com/getloopscale/loop-runtime/api/internal/release"

	"github.com/getloopscale/loop-runtime/api/internal/preview"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/policy"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/github"
	ghwebhook "github.com/getloopscale/loop-runtime/api/internal/github/webhook"
	"github.com/getloopscale/loop-runtime/api/internal/integrations/integrationsadapter"
	"github.com/getloopscale/loop-runtime/api/internal/integrations/integrationshttp"
	"github.com/getloopscale/loop-runtime/api/internal/repositories/repositoriesadapter"
	"github.com/getloopscale/loop-runtime/api/internal/repositories/repositorieshttp"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
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
	dbAdapter, crossTenant *db.PgxAdapter,
	stores *StoreDeps,
	orch *builds.BuildOrchestrator,
	k8sDeps *K8sDeps,
	cfg config.Config,
	buildQuota github.BuildQuota,
	shutdownCtx context.Context,
	logger *slog.Logger,
) *GitHubDeps {
	// Repository store for webhook lookups and CRUD. Required for the
	// RepositoryHandler — always created when the DB is reachable.
	repoStore := newRepositoryStore(crossTenant)
	repoHandler := repositorieshttp.NewRepositoryHandler(&repositoriesadapter.RepoStoreAdapter{Store: repoStore})

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
		statusNotifier := github.NewStatusNotifier(ghAppClient, &repositoriesadapter.RepoLookupAdapter{Store: repoStore}, &buildsadapter.BuildLookupAdapter{Store: stores.BuildStore}, commitStatusDomain(cfg), logger)
		if statusNotifier != nil {
			release.RegisterReleaseActivatedCallback(statusNotifier.OnBuildSucceeded)
			release.RegisterBuildFailedCallback(statusNotifier.OnBuildFailed)
			release.RegisterDeployFailedCallback(func(ctx context.Context, failure release.DeployFailure) {
				statusNotifier.OnDeployFailed(ctx, failure.TenantID, failure.ProjectName, failure.BuildID, failure.Reason, failure.Superseded)
			})
			logger.Info("github: commit status callbacks registered (global)")
		}
		// An installation is bound to a tenant by the claim endpoint, after
		// GitHub confirms through the App's user authorization that the user
		// can access it. Listing repositories and connecting one require that
		// binding.
		integrationsHandler = integrationshttp.NewIntegrationsGitHubHandler(
			&integrationsadapter.InstallationRepoListerAdapter{Client: ghAppClient},
			&integrationsadapter.InstallationLookupAdapter{Store: repoStore},
		).WithInstallationClaims(
			newUserInstallationSource(ghAppClient.AppID(), cfg, logger),
			&integrationsadapter.InstallationClaimAdapter{Store: repoStore},
		)
		logger.Info("github: integrations handler registered", "endpoint", "/api/v1/integrations/github/installations")
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
		buildSub = &buildsadapter.BuildSubmitterAdapter{
			Orchestrator: orch,
			Logger:       logger,
		}
	}
	var autoDeployer *github.AutoDeployer
	if ghAppClient != nil && buildSub != nil {
		autoDeployer = github.NewAutoDeployer(autoDeployConfig(cfg, ghAppClient, stores.ObjUploader, buildSub, buildQuota, logger))
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
		BuildApps: &previewadapter.PreviewAppsLookupAdapter{Builds: stores.BuildStore},
		// LIVE catch 2026-06-07 J-23 bug #4: PreviewCRCreator interface
		// had no production wiring. Canonical manager lives in
		// internal/k8s next to TenantCRManager, sharing the namespace
		// constant + dynamic client.
		CRCreator:           &previewadapter.PreviewCRCreatorAdapter{Mgr: k8sDeps.PreviewCRMgr},
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
		previewDeployer := github.NewPreviewDeployer(previewSvc, &repositoriesadapter.ConnectedRepoAdapter{Store: repoStore}, logger).
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

// newUserInstallationSource builds the GitHub side of the installation claim
// from the App's OAuth client credentials (GITHUB_APP_CLIENT_ID and
// GITHUB_APP_CLIENT_SECRET, which arrive in the same Secret as the App's id
// and private key). It returns nil when either is missing: the claim endpoint
// then answers 503, and no installation can be bound until they are set.
func newUserInstallationSource(appID int64, cfg config.Config, logger *slog.Logger) integrationshttp.UserInstallationSource {
	client, err := github.NewUserAccessClient(github.UserAccessConfig{
		AppID:        appID,
		ClientID:     os.Getenv("GITHUB_APP_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_APP_CLIENT_SECRET"),
		WebBaseURL:   cfg.GitHubWebBaseURL,
		APIBaseURL:   cfg.GitHubAPIBaseURL,
	})
	if err != nil {
		logger.Warn("github: GITHUB_APP_CLIENT_ID or GITHUB_APP_CLIENT_SECRET not set; installations cannot be claimed and no repository can be connected",
			"error", err)
		return nil
	}
	return &integrationsadapter.UserInstallationSourceAdapter{Client: client}
}

// newRepositoryStore builds the store of GitHub installations and repository
// links on the cross-tenant pool. Both are global objects: the webhook arrives
// with no tenant and resolves the owner from them, and the API's authorization
// gates ask which tenant owns an installation or a repository before letting
// the caller act. Over the request pool, scoped to the caller, another tenant's
// installation is hidden and "no owner" read as "not claimed" — the owner check
// passed. The API-path queries carry their own tenant predicate
// (queries/repositories.sql), and the handlers enforce ownership.
func newRepositoryStore(crossTenant db.DBTX) *store.PostgresRepositoryStore {
	return &store.PostgresRepositoryStore{DB: crossTenant}
}

// autoDeployConfig is the push deploy's configuration. It uploads source bundles to
// cfg.SourceBucket, the bucket a CLI upload uses, instead of the auto-deployer's default.
func autoDeployConfig(cfg config.Config, app *github.AppClient, objects github.ObjectUploader, builds github.BuildSubmitter, quota github.BuildQuota, logger *slog.Logger) github.AutoDeployConfig {
	return github.AutoDeployConfig{
		AppClient:      app,
		ObjectStore:    objects,
		BuildSubmitter: builds,
		BuildQuota:     quota,
		SourceBucket:   cfg.SourceBucket,
		Logger:         logger,
	}
}

// pushBuildQuota gates a push on the plan's builds per month with the limiter
// and the usage count the deploy route's gate reads.
type pushBuildQuota struct {
	limiter    *enforcement.PlanLimiter
	builds     enforcement.UsageCounter
	upgradeURL string
}

// newPushBuildQuota returns nil without a limiter: a boot with no database has
// no plan to enforce, the rule the route's middleware follows.
func newPushBuildQuota(limiter *enforcement.PlanLimiter, builds enforcement.UsageCounter, upgradeURL string) github.BuildQuota {
	if limiter == nil || builds == nil {
		return nil
	}
	return pushBuildQuota{limiter: limiter, builds: builds, upgradeURL: upgradeURL}
}

func (q pushBuildQuota) CheckBuildQuota(ctx context.Context, tenantID string) (github.BuildQuotaResult, error) {
	result, err := q.limiter.CheckUsage(ctx, tenantID, "builds_per_month", q.builds)
	if err != nil {
		return github.BuildQuotaResult{}, err
	}
	if result.Allowed {
		return github.BuildQuotaResult{Allowed: true}, nil
	}
	return github.BuildQuotaResult{Message: policy.PlanLimitMessage(result), UpgradeURL: q.upgradeURL}, nil
}

// commitStatusDomain is the domain the commit status's app link is composed on:
// the public domain users reach the app on, the base domain only where no public
// one is configured.
func commitStatusDomain(cfg config.Config) string {
	return release.UserFacingDomain(cfg.PublicDomain, cfg.BaseDomain)
}
