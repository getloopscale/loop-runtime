// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/pkg/domain"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// SyncCallbackActivator processes ArgoCD sync completion callbacks.
// When ArgoCD notifies that an Application is Synced + Healthy, this
// finds PENDING releases for that application and activates them.
//
// This is the push-based replacement for Phase D polling.
// MEET-2026-03-27-012: ArgoCD notifies instead of API polling.
// publicDomain is added per release-blockers-2026-06-25-set v1.1 #61.
type SyncCallbackActivator struct {
	releaseStore   ReleaseConvergenceStore
	envTargetStore store.EnvironmentTargetStore
	locker         deploycontracts.EnvironmentLocker
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	// J-16 slice 3: transactional-outbox writer (set via WithDeployEventStore).
	// Nil-safe: ActivationKernel.Activate skips the deploy.succeeded emit when nil.
	deployEventStore store.DeployEventStore
	baseDomain       string
	// publicDomain is the operator-configured wildcard root mirrored from
	// LOOPZERO_PUBLIC_DOMAIN (e.g. `prod-dev.loopzero.example`). When set, the
	// deploy.completed SSE event URL uses this domain instead of the legacy
	// baseDomain (`loopzero.local`) — closes loopzero-cloud#61 so the URL `loopzero deploy`
	// announces is the SAME publicly-resolvable URL `loopzero status` returns.
	publicDomain string
	logger       *slog.Logger
	// appLabelReader resolves tenant and environment from the loopzero.io/tenant
	// and loopzero.io/environment labels on the ArgoCD Application CR — the only
	// decoder of an Application's identity (see lookupTenantEnv).
	appLabelReader ArgoAppLabelReader
}

// WithAppLabelReader wires the label-based tenant/environment lookup.
// Production passes adapters.NewDynamicArgoAppLabelReader(mgmtDynamic, "argocd") in
// setup_orchestrator.go. Without a reader the callback cannot identify the
// release, and activation is left to the convergence loop.
func (a *SyncCallbackActivator) WithAppLabelReader(r ArgoAppLabelReader) *SyncCallbackActivator {
	a.appLabelReader = r
	return a
}

// errAppIdentityUnresolved is returned when the Application's tenant and
// environment cannot be read from its labels. The callback is the fast path;
// the convergence loop still activates the release.
var errAppIdentityUnresolved = &loopzeroerrors.LoopZeroError{
	Code:    "APP_IDENTITY_UNRESOLVED",
	Message: "sync callback: application identity not resolvable from its labels",
}

// lookupTenantEnv reads the tenant and environment from the Application CR's
// labels, which the tenant operator sets authoritatively.
//
// There is deliberately no fallback that parses the application name. The name
// is naming.ArgoAppName's "{tenant}-{project}-{env}" (#271), where every segment
// may contain hyphens, so no positional parse inverts it. The one that used to
// run here split on the LAST hyphen — the pre-#271 two-segment format — and for
// "tn-acme-web-production" answered tenant="tn-acme-web", which matched no
// release: activation silently degraded to the convergence loop while the
// callback reported nothing wrong.
func (a *SyncCallbackActivator) lookupTenantEnv(ctx context.Context, appName string) (tenantID, env string, err error) {
	if a.appLabelReader == nil {
		return "", "", fmt.Errorf("%w: no label reader wired (app %q)", errAppIdentityUnresolved, appName)
	}
	labels, readErr := a.appLabelReader.GetAppLabels(ctx, appName)
	if readErr != nil {
		return "", "", fmt.Errorf("%w: read labels of app %q: %w", errAppIdentityUnresolved, appName, readErr)
	}
	tenantID, env = labels[naming.LabelTenant], labels[naming.LabelEnvironment]
	if tenantID == "" || env == "" {
		return "", "", fmt.Errorf("%w: app %q lacks %s/%s labels", errAppIdentityUnresolved, appName,
			naming.LabelTenant, naming.LabelEnvironment)
	}
	return tenantID, env, nil
}

// NewSyncCallbackActivator creates a new SyncCallbackActivator.
//
// release-blockers-2026-06-25-set v1.1 #61: publicDomain is the
// canonical public wildcard (LOOPZERO_PUBLIC_DOMAIN) used to compose the
// URL announced in the deploy.completed SSE event. Empty publicDomain
// falls back to baseDomain (legacy default `loopzero.local`) so test fixtures
// + envs without a public wildcard preserve pre-#61 behavior.
func NewSyncCallbackActivator(
	releaseStore ReleaseConvergenceStore,
	envTargetStore store.EnvironmentTargetStore,
	locker deploycontracts.EnvironmentLocker,
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	},
	baseDomain, publicDomain string,
	logger *slog.Logger,
) *SyncCallbackActivator {
	return &SyncCallbackActivator{
		releaseStore:   releaseStore,
		envTargetStore: envTargetStore,
		locker:         locker,
		eventStore:     eventStore,
		baseDomain:     baseDomain,
		publicDomain:   publicDomain,
		logger:         logger,
	}
}

// WithDeployEventStore wires the transactional-outbox writer (J-16 slice 3).
// Fluent setter so existing call sites/tests are unaffected; the composition
// root sets it in production. Nil = deploy.succeeded outbox emit skipped.
func (a *SyncCallbackActivator) WithDeployEventStore(s store.DeployEventStore) *SyncCallbackActivator {
	a.deployEventStore = s
	return a
}

// PendingReleaseByTenantEnvFinder is an optional interface for efficient tenant+env lookup.
// Falls back to FindPendingReleases + Go-side filtering if not implemented (CODE-SCAN-001).
type PendingReleaseByTenantEnvFinder interface {
	FindPendingReleasesByTenantEnv(ctx context.Context, tenantID, env string) ([]db.Release, error)
}

// OnSyncCompleted is called when ArgoCD reports an Application as Synced + Healthy.
// The appName is in format "{tenantID}-{environment}" (e.g., "tn-loopzero-production").
// It finds all PENDING releases matching that tenant+environment and activates them.
func (a *SyncCallbackActivator) OnSyncCompleted(appName, revision string) error {
	// T6.6 of deep-review-remediation-plan (closes find_code_007 / CODE-007 LOW):
	// the original "ARCH-006 fix" comment referenced an ARCH-006 ID that does
	// not exist in the architecture review (current ARCH-006 is the unrelated
	// CustomStrategy.DockerfileStep finding). The intent is real: callback
	// activation must NOT be cancelled by the inbound HTTP request context
	// (which is short-lived and may close before activation finishes,
	// orphaning the release in PENDING). A bounded 30s context.Background()
	// is the simplest correct choice.
	//
	// TODO(T6.4 + T1.3): once OTel trace propagation is wired through the
	// ArgoCD callback handler, replace context.Background() with a context
	// derived from the inbound trace so spans correlate end-to-end.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantID, env, err := a.lookupTenantEnv(ctx, appName)
	if err != nil {
		return err
	}
	// The callback is authenticated as the GitOps controller, which acts for
	// every tenant, and carries no tenant scope. It acts here for the tenant
	// the platform labelled the Application with; the release stores run on
	// the request pool, whose tenant tables are fail-closed under RLS.
	ctx = dbtx.WithTenantScope(ctx, tenantID)

	// CODE-SCAN-001 fix: Use tenant+env-filtered query when available,
	// avoiding full table scan of all PENDING releases.
	var releases []db.Release
	if finder, ok := a.releaseStore.(PendingReleaseByTenantEnvFinder); ok {
		releases, err = finder.FindPendingReleasesByTenantEnv(ctx, tenantID, env)
	} else {
		// Fallback path: scan PENDING releases in the default cell.
		// Real production always hits the efficient tenant+env path above;
		// this branch only fires in tests with simple store fakes. We pass
		// domain.DefaultCellID because the callback itself does not carry cell
		// information — if a caller needs cross-cell callback routing, the
		// efficient finder must be implemented on the store.
		var allReleases []db.Release
		allReleases, err = a.releaseStore.FindPendingReleases(ctx, domain.DefaultCellID)
		if err == nil {
			for _, r := range allReleases {
				if r.TenantID == tenantID && r.Environment == env {
					releases = append(releases, r)
				}
			}
		}
	}
	if err != nil {
		return fmt.Errorf("sync callback: find pending releases: %w", err)
	}

	// T1.7 of deep-review-2026-05-10-remediation-plan (find_callback_false_success HIGH):
	// collect per-release activation errors so a partial failure surfaces
	// as HTTP 500 → ArgoCD retries. Pre-T1.7 the function returned nil
	// even when 1/N activations failed, causing the release to stay
	// PENDING with no retry signal.
	activated := 0
	var failedReleases []string
	for _, release := range releases {
		// MEET-2026-03-31-024 BUG-03 defense-in-depth: if GitCommit is empty,
		// do NOT activate via callback — let the convergence loop handle it.
		// Empty GitCommit means SetGitCommit failed (now fatal) or a race occurred.
		// Activating with empty GitCommit bypasses revision verification.
		if release.GitCommit == "" {
			a.logger.Warn("sync callback: release has empty GitCommit, deferring to convergence loop",
				"release_id", release.ID)
			continue
		}

		// Verify revision matches
		if revision != "" && release.GitCommit != revision {
			a.logger.Warn("sync callback: revision mismatch, skipping",
				"release_id", release.ID, "expected", release.GitCommit, "actual", revision)
			continue
		}

		if err := a.activateRelease(ctx, &release); err != nil {
			a.logger.Error("sync callback: activation failed",
				"release_id", release.ID, "error", err)
			failedReleases = append(failedReleases, release.ID)
			continue
		}
		activated++
	}

	if activated > 0 {
		a.logger.Info("sync callback: releases activated",
			"app", appName, "count", activated, "revision", revision)
	}

	if len(failedReleases) > 0 {
		return fmt.Errorf("sync callback: %d release(s) failed to activate: %v (ArgoCD should retry)",
			len(failedReleases), failedReleases)
	}

	return nil
}

func (a *SyncCallbackActivator) activateRelease(ctx context.Context, release *db.Release) error {
	return a.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		apps := releaseAppsToActivationApps(release, a.baseDomain, a.publicDomain)
		duration := int(time.Since(release.CreatedAt).Seconds())
		return ActivationKernel{
			Releases: a.releaseStore, EnvTargets: a.envTargetStore, Events: a.eventStore,
			DeployEvents: a.deployEventStore, Logger: a.logger,
		}.Activate(ctx, tx, release, apps, duration)
	})
}
