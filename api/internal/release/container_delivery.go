// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/events"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/naming"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// GitManifestReader abstracts reading manifests from git history (DIP for rollback).
type GitManifestReader interface {
	GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error)
}

// ContainerDeliveryActivator implements DeliveryActivator for container-based
// deployments (Kubernetes pods via Gitea + ArgoCD).
//
// This is the existing delivery model extracted into the interface pattern
// (MEET-2026-03-26-009 Fase 0). The 5-phase activation and rollback logic
// are identical to the pre-refactoring code — zero behavior change.
type ContainerDeliveryActivator struct {
	releaseStore   db.ReleaseStore
	envTargetStore store.EnvironmentTargetStore
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	sync              deploycontracts.SyncClient
	locker            deploycontracts.EnvironmentLocker
	gitops            deploycontracts.GitOpsPusher
	gitReader         GitManifestReader
	argocdSyncTimeout time.Duration
	// Sprint 6 / T3.1: optional per-app attestation store. When set,
	// phaseACreateRelease persists per-app signature + SBOM metadata to
	// release_app_attestations. Nil-safe: if absent, the activator falls
	// back to the legacy releases.image_* columns only.
	appAttestationStore store.AppAttestationStore
	// J-16 slice 3: transactional-outbox writer. When set, ActivationKernel.Activate
	// stages a deploy.succeeded event on the activation tx. Nil-safe (skipped).
	deployEventStore store.DeployEventStore
}

// ContainerDeliveryDeps holds dependencies for ContainerDeliveryActivator.
type ContainerDeliveryDeps struct {
	ReleaseStore   db.ReleaseStore
	EnvTargetStore store.EnvironmentTargetStore
	EventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Sync              deploycontracts.SyncClient
	Locker            deploycontracts.EnvironmentLocker
	GitOps            deploycontracts.GitOpsPusher
	GitReader         GitManifestReader
	ArgocdSyncTimeout time.Duration
	// Sprint 6 / T3.1: per-app attestation store. May be nil during
	// migration windows; phaseACreateRelease will skip per-app persistence
	// and rely on the legacy releases.image_* columns.
	AppAttestationStore store.AppAttestationStore
	// J-16 slice 3: transactional-outbox writer for deploy.succeeded events.
	// May be nil (outbox emit skipped).
	DeployEventStore store.DeployEventStore
}

// NewContainerDeliveryActivator creates a new ContainerDeliveryActivator.
func NewContainerDeliveryActivator(deps ContainerDeliveryDeps) *ContainerDeliveryActivator {
	syncTimeout := deps.ArgocdSyncTimeout
	if syncTimeout == 0 {
		syncTimeout = 600 * time.Second
	}
	return &ContainerDeliveryActivator{
		releaseStore:        deps.ReleaseStore,
		envTargetStore:      deps.EnvTargetStore,
		eventStore:          deps.EventStore,
		sync:                deps.Sync,
		locker:              deps.Locker,
		gitops:              deps.GitOps,
		gitReader:           deps.GitReader,
		argocdSyncTimeout:   syncTimeout,
		appAttestationStore: deps.AppAttestationStore,
		deployEventStore:    deps.DeployEventStore,
	}
}

// Activate runs the 5-phase container release activation.
// Phases A-E are semantically identical to the pre-refactoring activateRelease.
func (c *ContainerDeliveryActivator) Activate(ctx context.Context, build deploycontracts.ActivationBuild, result deploycontracts.BuildCallbackRequest) error {
	// Phase A (Lock): Create release record (READY)
	release, err := c.phaseACreateRelease(ctx, build, result)
	if err != nil {
		return err
	}

	// Phase B (no lock): Push manifests to GitOps repo
	commitSHA, pushErr := c.phaseBDeliver(ctx, build, release, result)

	// Phase C (Lock): Record delivery result (PENDING or GIT_MIRROR_FAILED)
	deliveryFailed, err := c.phaseCRecordResult(ctx, build, release, commitSHA, pushErr)
	if err != nil {
		return err
	}
	if deliveryFailed {
		// Return error so BuildOrchestrator keeps build in RELEASING for retry
		return fmt.Errorf("delivery failed: gitops push error")
	}

	// Phase D is now ASYNC (MEET-2026-03-28-015).
	// Release activation is handled by:
	//   1. ArgoCD Notification callback (fast path, seconds)
	//   2. ReleaseConvergenceLoop (safety net, polls every 10s)
	//   3. ReleaseReaper (ultimate timeout, 10m)
	//
	// We trigger ArgoCD sync once here, then return immediately.
	// The release stays in PENDING state until async activation.
	argoAppName, nameErr := naming.ArgoAppName(build.TenantID, build.ProjectID, build.Environment)
	if nameErr != nil {
		return fmt.Errorf("argocd application name: %w", nameErr)
	}
	if err := c.triggerSyncWithRetry(ctx, argoAppName); err != nil {
		// MEET-2026-04-04-029 P3: 404 (app not found) is recoverable — the TenantController
		// may not have created the ArgoCD Application yet. Leave release in PENDING and let
		// the convergence loop retrigger sync once the Application CR exists.
		if errors.Is(err, deploycontracts.ErrSyncApplicationNotFound) || errors.Is(err, loopzeroerrors.ErrDeployServiceAuthFailed) {
			slog.Warn("argocd sync trigger failed (recoverable) — release stays PENDING for convergence loop retry",
				"build_id", build.ID, "app", argoAppName, "error", err,
				"hint", "If this persists, verify the environment exists for this tenant and ArgoCD credentials are valid")
			// Don't fail the release — convergence loop will retrigger
		} else {
			slog.Error("argocd trigger sync failed after retries",
				"build_id", build.ID, "app", argoAppName, "error", err)
			// Non-404 failure — fail the release immediately.
			if finalizeErr := c.phaseEFinalize(ctx, build, release, result, true); finalizeErr != nil {
				return finalizeErr
			}
			return fmt.Errorf("sync trigger failed: %w", deploycontracts.ErrActivationAlreadyHandled)
		}
	}

	// Emit deploy.verifying to show async verification started
	verifyingData := events.SafeJSONMarshal(map[string]interface{}{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "verifying",
		"app":       argoAppName,
	})
	if _, emitErr := c.eventStore.Append(ctx, build.ID, "deploy.verifying", verifyingData); emitErr != nil {
		slog.Warn("failed to emit deploy.verifying event", "build_id", build.ID, "error", emitErr)
	}

	// Release is PENDING — async paths will activate and emit deploy.completed
	return nil
}

// Rollback delivers a previous release's manifests to the runtime.
// Fetches old manifests from Gitea git history, pushes them, and triggers ArgoCD sync.
// State machine transitions are handled by the RollbackOrchestrator, not here.
func (c *ContainerDeliveryActivator) Rollback(ctx context.Context, req deploycontracts.RollbackDeliveryRequest) error {
	if req.TargetRelease.GitCommit == "" {
		return fmt.Errorf("container rollback: target release has no git commit")
	}

	// Fetch old manifests from git
	manifests, err := c.gitReader.GetManifestsAtCommit(ctx, req.TenantID, req.ProjectID, req.Environment, req.TargetRelease.GitCommit)
	if err != nil {
		return fmt.Errorf("container rollback: fetch manifests: %w", err)
	}

	// Push old manifests to gitops repo
	_, err = c.gitops.Push(ctx, deploycontracts.GitOpsPushOpts{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		Manifests:     string(manifests),
		ReleaseID:     req.TargetRelease.ID,
		CorrelationID: req.CorrelationID,
	})
	if err != nil {
		return fmt.Errorf("container rollback: push manifests: %w", err)
	}

	// Trigger the sync on the Application the TenantController creates via
	// EnsureApplication; its identity is tenant + project + env, from
	// naming.ArgoAppName (#271, MEET-2026-03-27-012). A failure here fails the
	// rollback — a rollback that does not sync is not a rollback.
	argoAppName, nameErr := naming.ArgoAppName(req.TenantID, req.ProjectID, req.Environment)
	if nameErr != nil {
		return fmt.Errorf("argocd application name: %w", nameErr)
	}
	if syncErr := c.sync.TriggerSync(ctx, argoAppName); syncErr != nil {
		return fmt.Errorf("container rollback: sync trigger failed: %w", syncErr)
	}
	// LIVE-caught 2026-06-18 sponsor mandate: DB safety invariant —
	// wait for sync confirmation before letting the orchestrator's
	// Phase 5 flip release state. Without this wait the rollback
	// would return success while the deployment infrastructure was
	// still applying manifests + pods could still serve the old
	// (rolled-back) code.
	if waitErr := c.sync.WaitForSync(ctx, argoAppName, c.argocdSyncTimeout, nil); waitErr != nil {
		return fmt.Errorf("container rollback: sync wait failed: %w", waitErr)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Phase implementations (extracted from release_activator.go, same logic)
// ---------------------------------------------------------------------------

func (c *ContainerDeliveryActivator) phaseACreateRelease(ctx context.Context, build deploycontracts.ActivationBuild, result deploycontracts.BuildCallbackRequest) (*db.Release, error) {
	var release *db.Release

	err := c.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		appsJSON, err := json.Marshal(result.Apps)
		if err != nil {
			return fmt.Errorf("activate release: marshal apps: %w", err)
		}

		var artifactsJSON json.RawMessage
		if result.Manifests != "" {
			artifactsJSON, err = json.Marshal(map[string]string{
				"manifests": result.Manifests,
			})
			if err != nil {
				return fmt.Errorf("activate release: marshal artifacts: %w", err)
			}
		}

		release = &db.Release{
			BuildID:     build.ID,
			TenantID:    build.TenantID,
			ProjectID:   build.ProjectID,
			Environment: build.Environment,
			CreatedBy:   build.CreatedBy,
			Apps:        appsJSON,
			Artifacts:   artifactsJSON,
		}

		// CHG-2026-04-25-ATTESTATION-WIRE Phase 5: persist supply-chain
		// attestation columns from the first app's enriched data
		// (Phase 4). Multi-app builds: only Apps[0] flows to the release
		// row — multi-app per-release attestation is out of scope until
		// admission policy needs it. Documented in ADR-0014.
		// Defensive: only set ImageSignedAt when ImageSignature is also
		// present (a timestamp without a signature reference is
		// meaningless for verification).
		if len(result.Apps) > 0 {
			first := result.Apps[0]
			if first.ImageDigestSHA256 != "" {
				digest := first.ImageDigestSHA256
				release.ImageDigest = &digest
			}
			if first.ImageSignature != "" {
				sig := first.ImageSignature
				release.ImageSignature = &sig
			}
			if first.ImageSignedAt != nil && first.ImageSignature != "" {
				release.ImageSignedAt = first.ImageSignedAt
			}
			// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
			// Per ADR D1: SLSA L3 attestation chain — Rekor URL of
			// cosign-attest step persisted alongside signature for
			// downstream verification (loopzero deploy --verify-attestation).
			// Empty when CosignAttestEnabled=false (default) or attest
			// step never ran. Empty release column stays NULL.
			if first.ImageAttestationRekorURL != "" {
				url := first.ImageAttestationRekorURL
				release.ImageAttestationRekorURL = &url
			}
		}

		if err := c.releaseStore.Create(ctx, tx, release); err != nil {
			return fmt.Errorf("activate release: create release: %w", err)
		}

		// Sprint 6 / T3.1: persist per-app attestation (signature + SBOM)
		// for ALL apps in the build, not just Apps[0]. This is the data
		// the admission policy needs (D5: "ALL images signed"). The store
		// is optional during migration windows; absence is non-fatal.
		if c.appAttestationStore != nil && len(result.Apps) > 0 {
			atts := make([]store.AppAttestation, 0, len(result.Apps))
			sbomCount := 0
			for _, app := range result.Apps {
				if app.Name == "" || app.ImageDigestSHA256 == "" {
					continue // skip apps without a real digest
				}
				att := store.AppAttestation{
					AppName:     app.Name,
					ImageDigest: app.ImageDigestSHA256,
				}
				if app.ImageSignature != "" {
					sig := app.ImageSignature
					att.ImageSignature = &sig
				}
				if app.ImageSignedAt != nil && app.ImageSignature != "" {
					ts := *app.ImageSignedAt
					att.SignedAt = &ts
				}
				if app.SBOMS3Key != "" {
					k := app.SBOMS3Key
					att.SBOMS3Key = &k
				}
				if app.SBOMFormat != "" {
					f := app.SBOMFormat
					att.SBOMFormat = &f
				}
				if app.SBOMComponentCount > 0 {
					n := app.SBOMComponentCount
					att.SBOMComponentCount = &n
				}
				atts = append(atts, att)
				if app.SBOMS3Key != "" {
					sbomCount++
				}

				// Sprint 6 / T3.2: per-app attestation metric.
				signed := "false"
				if att.SignedAt != nil {
					signed = "true"
				}
				metrics.ReleaseAppAttestationTotal.WithLabelValues(
					build.TenantID, build.Environment, app.Name, signed,
				).Inc()
			}
			if len(atts) > 0 {
				// Plan T2.4 D7 — pass tx so release + attestations commit atomically.
				// Previously SaveAppAttestations ran on a separate connection and
				// raced ahead of the release commit, producing FK violation
				// SQLSTATE 23503 in dogfood logs (F-S2-01). Now both rows live in
				// the same transaction; if attestation insert fails the release
				// rollback prevents orphaned rows.
				if saveErr := c.appAttestationStore.SaveAppAttestationsTx(
					ctx, tx, release.ID, build.TenantID, atts,
				); saveErr != nil {
					metrics.ReleaseAppAttestationSaveFailTotal.WithLabelValues(build.Environment).Inc()
					slog.Warn("attestation persist failed (non-blocking) — release proceeds without per-app attestation rows",
						"release_id", release.ID,
						"tenant_id", build.TenantID,
						"environment", build.Environment,
						"attestation_count", len(atts),
						"error", saveErr)
				}
			}
			// Sprint 6 / T3.2: SBOM generation counter. One increment
			// per release, regardless of how many apps had SBOMs (count is
			// in the metric value via apps; here we count releases with
			// at least one SBOM).
			if sbomCount > 0 {
				metrics.SBOMGeneratedTotal.WithLabelValues(
					build.TenantID, build.Environment,
				).Inc()
			}
		}

		// CHG-2026-04-25-ATTESTATION-WIRE Phase 7: emit observability
		// metric so dashboards can detect signing regressions even when
		// the deploy itself stays green (signature missing != deploy
		// failure, by design — see Phase 5 non-fatal contract).
		signed := "false"
		if release.ImageSignedAt != nil {
			signed = "true"
		}
		metrics.ReleaseSignedTotal.WithLabelValues(build.TenantID, build.Environment, signed).Inc()

		return nil
	})

	if err != nil {
		return nil, err
	}
	return release, nil
}

func (c *ContainerDeliveryActivator) phaseBDeliver(ctx context.Context, build deploycontracts.ActivationBuild, release *db.Release, result deploycontracts.BuildCallbackRequest) (string, error) {
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	return c.gitops.Push(pushCtx, deploycontracts.GitOpsPushOpts{
		TenantID:      build.TenantID,
		ProjectID:     build.ProjectID,
		Environment:   build.Environment,
		Manifests:     result.Manifests,
		BuildID:       build.ID,
		ReleaseID:     release.ID,
		CorrelationID: build.CorrelationID,
	})
}

func (c *ContainerDeliveryActivator) phaseCRecordResult(ctx context.Context, build deploycontracts.ActivationBuild, release *db.Release, commitSHA string, pushErr error) (deliveryFailed bool, err error) {
	return deliveryFailed, c.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if pushErr != nil {
			slog.Error("gitops push failed",
				"build_id", build.ID,
				"release_id", release.ID,
				"error", pushErr,
			)

			if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.RPEventGitMirrorFailed)); err != nil {
				return fmt.Errorf("activate release: transition to GIT_MIRROR_FAILED: %w", err)
			}

			failedData := events.SafeJSONMarshal(map[string]interface{}{
				"deploy_id": deriveDeployID(release.ID),
				"status":    "delivery_failed",
				"error":     pushErr.Error(),
			})
			if _, err := c.eventStore.Append(ctx, build.ID, "deploy.delivery_failed", failedData); err != nil {
				return fmt.Errorf("activate release: emit deploy.delivery_failed: %w", err)
			}

			deliveryFailed = true
			return nil
		}

		// MEET-2026-03-31-024: SetGitCommit MUST be fatal.
		// If GitCommit is not recorded, the SyncCallbackActivator revision guard
		// (release.GitCommit != revision) is bypassed when GitCommit is empty,
		// allowing wrong-version activation on rapid sequential deploys.
		if commitSHA != "" {
			if err := c.releaseStore.SetGitCommit(ctx, tx, release.ID, commitSHA); err != nil {
				return fmt.Errorf("persist git_commit on release: %w", err)
			}
		}

		if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.RPEventGitMirrorCompleted)); err != nil {
			return fmt.Errorf("activate release: transition to PENDING: %w", err)
		}

		// SSE: deploy.delivering (delivery-agnostic name)
		pendingData := events.SafeJSONMarshal(map[string]string{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "delivering",
		})
		if _, err := c.eventStore.Append(ctx, build.ID, "deploy.delivering", pendingData); err != nil {
			return fmt.Errorf("activate release: emit deploy.delivering: %w", err)
		}

		// D2 of plan 2026-06-07-preview-as-category-single-slot-plan.md:
		// preview env_targets get kind='preview' so D5 enforcement
		// (CountPermanentEnvironments) doesn't count them against the
		// plan_limits.environments cap. Detection canonical via
		// naming.IsPreviewEnv("preview-pr-N"). Without this branch,
		// every preview build would still write kind='permanent' and
		// the Phase 2 enforcement fix would be incomplete (Phase 2
		// only filters COUNT; if rows are mis-kinded, counts are wrong).
		if naming.IsPreviewEnv(build.Environment) {
			if err := c.envTargetStore.SetPreviewDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
				return fmt.Errorf("activate release: set preview desired: %w", err)
			}
		} else {
			if err := c.envTargetStore.SetDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
				return fmt.Errorf("activate release: set desired: %w", err)
			}
		}

		return nil
	})
}

// phaseDVerify REMOVED (MEET-2026-03-28-015) — 81 lines of synchronous ArgoCD polling deleted.
// Replaced by async: ReleaseConvergenceLoop + SyncCallbackActivator.
// Historical reference: git show f1bf264:api/internal/services/container_delivery.go

func (c *ContainerDeliveryActivator) phaseEFinalize(ctx context.Context, build deploycontracts.ActivationBuild, release *db.Release, result deploycontracts.BuildCallbackRequest, verifyFailed bool) error {
	logger := slog.With("release_id", release.ID, "build_id", build.ID)
	return c.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if verifyFailed {
			if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.RPEventSyncFailed)); err != nil {
				return fmt.Errorf("activate release: transition to SYNC_FAILED: %w", err)
			}

			// MEET-2026-04-04-029 P6: Use product language, not internal terms.
			failedData := events.SafeJSONMarshal(map[string]interface{}{
				"deploy_id": deriveDeployID(release.ID),
				"status":    "failed",
				"reason":    "Deploy verification failed. The service did not become healthy after sync. Run `loopzero logs` to check application errors.",
			})
			if _, err := c.eventStore.Append(ctx, build.ID, "deploy.sync_failed", failedData); err != nil {
				return fmt.Errorf("activate release: emit deploy.sync_failed: %w", err)
			}

			return nil
		}

		// Convert build callback apps to activation format (includes URLs).
		apps := make([]ActivationAppInfo, 0, len(result.Apps))
		for _, app := range result.Apps {
			apps = append(apps, ActivationAppInfo{
				Name:   app.Name,
				Status: "deployed",
				URL:    app.URL,
			})
		}
		durationSecs := int(time.Since(build.CreatedAt).Seconds())

		return ActivationKernel{
			Releases: c.releaseStore, EnvTargets: c.envTargetStore, Events: c.eventStore,
			DeployEvents: c.deployEventStore, Logger: logger,
		}.Activate(ctx, tx, release, apps, durationSecs)
	})
}

// containerSyncPolicy is the budget container delivery gives the initial sync.
//
// A stuck operation gets 8 attempts on a 5s linear cadence (~35s): during a
// burst of deploys the operation lock is usually a peer's sync finishing, and
// the SyncSerializer already serialises per application, so waiting drains the
// queue. The stuck operation is terminated only before the FINAL attempt, so an
// earlier attempt never cancels a peer's legitimate sync (V9 dogfood 2026-05-29/30).
// Any other failure keeps a 3-attempt exponential budget (1s, 2s) so a genuine
// outage fails fast.
var containerSyncPolicy = deploycontracts.SyncRecoveryPolicy{
	MaxAttempts:         3,
	MaxStuckAttempts:    8,
	TerminateAfterStuck: 7,
	Backoff: func(failures int, stuck bool) time.Duration {
		if stuck {
			return 5 * time.Second
		}
		return time.Duration(1<<uint(failures-1)) * time.Second
	},
}

func (c *ContainerDeliveryActivator) triggerSyncWithRetry(ctx context.Context, appName string) error {
	return deploycontracts.TriggerSyncWithRecovery(ctx, c.sync, appName, containerSyncPolicy, slog.Default())
}
