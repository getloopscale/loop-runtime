// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/metrics"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
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
	envTargetStore db.EnvironmentTargetStore
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	sync              SyncClient
	locker            EnvironmentLocker
	gitops            GitOpsPusher
	gitReader         GitManifestReader
	argocdSyncTimeout time.Duration
	// Sprint 6 / T3.1: optional per-app attestation store. When set,
	// phaseACreateRelease persists per-app signature + SBOM metadata to
	// release_app_attestations. Nil-safe: if absent, the activator falls
	// back to the legacy releases.image_* columns only.
	appAttestationStore db.AppAttestationStore
}

// ContainerDeliveryDeps holds dependencies for ContainerDeliveryActivator.
type ContainerDeliveryDeps struct {
	ReleaseStore      db.ReleaseStore
	EnvTargetStore    db.EnvironmentTargetStore
	EventStore        interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Sync              SyncClient
	Locker            EnvironmentLocker
	GitOps            GitOpsPusher
	GitReader         GitManifestReader
	ArgocdSyncTimeout time.Duration
	// Sprint 6 / T3.1: per-app attestation store. May be nil during
	// migration windows; phaseACreateRelease will skip per-app persistence
	// and rely on the legacy releases.image_* columns.
	AppAttestationStore db.AppAttestationStore
}

// NewContainerDeliveryActivator creates a new ContainerDeliveryActivator.
func NewContainerDeliveryActivator(deps ContainerDeliveryDeps) *ContainerDeliveryActivator {
	syncTimeout := deps.ArgocdSyncTimeout
	if syncTimeout == 0 {
		syncTimeout = 600 * time.Second
	}
	return &ContainerDeliveryActivator{
		releaseStore:      deps.ReleaseStore,
		envTargetStore:    deps.EnvTargetStore,
		eventStore:        deps.EventStore,
		sync:              deps.Sync,
		locker:            deps.Locker,
		gitops:            deps.GitOps,
		gitReader:         deps.GitReader,
		argocdSyncTimeout: syncTimeout,
		appAttestationStore: deps.AppAttestationStore,
	}
}

// Activate runs the 5-phase container release activation.
// Phases A-E are semantically identical to the pre-refactoring activateRelease.
func (c *ContainerDeliveryActivator) Activate(ctx context.Context, build *db.Build, result BuildCallbackRequest) error {
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
	argoAppName := fmt.Sprintf("%s-%s", build.TenantID, build.Environment)
	if err := c.triggerSyncWithRetry(ctx, argoAppName); err != nil {
		// MEET-2026-04-04-029 P3: 404 (app not found) is recoverable — the TenantController
		// may not have created the ArgoCD Application yet. Leave release in PENDING and let
		// the convergence loop retrigger sync once the Application CR exists.
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "DEPLOY_SERVICE_AUTH_FAILED") {
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
			return fmt.Errorf("sync trigger failed: %w", ErrActivationAlreadyHandled)
		}
	}

	// Emit deploy.verifying to show async verification started
	verifyingData := safeJSONMarshal(map[string]interface{}{
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
func (c *ContainerDeliveryActivator) Rollback(ctx context.Context, req RollbackDeliveryRequest) error {
	if req.TargetRelease.GitCommit == "" {
		return fmt.Errorf("container rollback: target release has no git commit")
	}

	// Fetch old manifests from git
	manifests, err := c.gitReader.GetManifestsAtCommit(ctx, req.TenantID, req.ProjectID, req.Environment, req.TargetRelease.GitCommit)
	if err != nil {
		return fmt.Errorf("container rollback: fetch manifests: %w", err)
	}

	// Push old manifests to gitops repo
	_, err = c.gitops.Push(ctx, GitOpsPushOpts{
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

	// Trigger ArgoCD sync (best-effort).
	// ArgoCD Application name is {tenantID}-{environment}, matching what the
	// TenantController creates via EnsureApplication. NOT ProjectName.
	// See MEET-2026-03-27-012 for the bug report.
	argoAppName := fmt.Sprintf("%s-%s", req.TenantID, req.Environment)
	if syncErr := c.sync.TriggerSync(ctx, argoAppName); syncErr != nil {
		slog.Warn("container rollback: argocd sync trigger failed, continuing", "error", syncErr)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Phase implementations (extracted from release_activator.go, same logic)
// ---------------------------------------------------------------------------

func (c *ContainerDeliveryActivator) phaseACreateRelease(ctx context.Context, build *db.Build, result BuildCallbackRequest) (*db.Release, error) {
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
			atts := make([]db.AppAttestation, 0, len(result.Apps))
			sbomCount := 0
			for _, app := range result.Apps {
				if app.Name == "" || app.ImageDigestSHA256 == "" {
					continue // skip apps without a real digest
				}
				att := db.AppAttestation{
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

func (c *ContainerDeliveryActivator) phaseBDeliver(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest) (string, error) {
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	return c.gitops.Push(pushCtx, GitOpsPushOpts{
		TenantID:      build.TenantID,
		ProjectID:     build.ProjectID,
		Environment:   build.Environment,
		Manifests:     result.Manifests,
		BuildID:       build.ID,
		ReleaseID:     release.ID,
		CorrelationID: build.CorrelationID,
	})
}

func (c *ContainerDeliveryActivator) phaseCRecordResult(ctx context.Context, build *db.Build, release *db.Release, commitSHA string, pushErr error) (deliveryFailed bool, err error) {
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

			failedData := safeJSONMarshal(map[string]interface{}{
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
		pendingData := safeJSONMarshal(map[string]string{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "delivering",
		})
		if _, err := c.eventStore.Append(ctx, build.ID, "deploy.delivering", pendingData); err != nil {
			return fmt.Errorf("activate release: emit deploy.delivering: %w", err)
		}

		if err := c.envTargetStore.SetDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
			return fmt.Errorf("activate release: set desired: %w", err)
		}

		return nil
	})
}

// phaseDVerify REMOVED (MEET-2026-03-28-015) — 81 lines of synchronous ArgoCD polling deleted.
// Replaced by async: ReleaseConvergenceLoop + SyncCallbackActivator.
// Historical reference: git show f1bf264:api/internal/services/container_delivery.go

func (c *ContainerDeliveryActivator) phaseEFinalize(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest, verifyFailed bool) error {
	logger := slog.With("release_id", release.ID, "build_id", build.ID)
	return c.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if verifyFailed {
			if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.RPEventSyncFailed)); err != nil {
				return fmt.Errorf("activate release: transition to SYNC_FAILED: %w", err)
			}

			// MEET-2026-04-04-029 P6: Use product language, not internal terms.
			failedData := safeJSONMarshal(map[string]interface{}{
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

		return ActivateReleaseKernel(ctx, tx, release, c.releaseStore, c.envTargetStore, c.eventStore, apps, durationSecs, logger)
	})
}

func (c *ContainerDeliveryActivator) triggerSyncWithRetry(ctx context.Context, appName string) error {
	// V9 dogfood 2026-05-29 unblock: per-error-type budget. Recoverable
	// errors (ErrSyncOperationInProgress) get 8 attempts with linear
	// backoff (let app-level lock queue drain naturally during dogfood
	// concurrency); non-recoverable errors keep the 3-attempt budget
	// (faster fail for genuine failures, preserves test contracts).
	const (
		maxAttempts = 3 // for non-recoverable errors (fail-fast on connection-refused etc)
		// V9 dogfood 2026-05-30 final tuning: 8 attempts × 5s linear
		// backoff = 40s per activation budget. Combined with reaper
		// maxActivationAttempts=10 × Interval=30s tick: ~5min total
		// in-flight budget per build. The SyncSerializer already
		// serializes per-app at the API layer; this budget covers
		// cumulative wait for upstream in-flight syncs.
		maxAttemptsSyncInProgress  = 8
		opInProgressLinearBackoffS = 5
	)
	var lastErr error
	for attempt := 0; ; attempt++ {
		err := c.sync.TriggerSync(ctx, appName)
		if err == nil {
			return nil
		}
		lastErr = err

		isOpInProgress := errors.Is(err, loopzeroerrors.ErrSyncOperationInProgress)
		budget := maxAttempts
		if isOpInProgress {
			budget = maxAttemptsSyncInProgress
		}
		if attempt+1 >= budget {
			break
		}

		// MEET-2026-03-29-019 + V9 concurrency-aware: for stuck
		// operations only terminate on the SECOND-TO-LAST attempt
		// (earlier attempts back off and let queue drain without
		// canceling a peer's sync; final attempt fails fast).
		if isOpInProgress && attempt == budget-2 {
			slog.Warn("argocd sync operation in progress — terminating stuck operation before final retry",
				"app", appName, "attempt", attempt+1)
			type terminator interface {
				TerminateOperation(ctx context.Context, appName string) error
			}
			if t, ok := c.sync.(terminator); ok {
				if termErr := t.TerminateOperation(ctx, appName); termErr != nil {
					slog.Error("failed to terminate stuck argocd operation",
						"app", appName, "error", termErr)
				} else {
					slog.Info("terminated stuck argocd operation",
						"app", appName)
				}
			}
		} else if isOpInProgress {
			slog.Info("argocd sync operation in progress — backing off to let queue drain",
				"app", appName, "attempt", attempt+1)
		} else {
			slog.Warn("argocd trigger sync failed, retrying",
				"app", appName, "attempt", attempt+1, "error", err)
		}

		var backoff time.Duration
		if isOpInProgress {
			// Linear backoff (predictable cadence): 5s, 5s, 5s, 5s, 5s, 5s, 5s
			// Total budget: ~35s for 8 attempts = fits in 60s service timeout
			// budget while letting an ArgoCD sync queue of ~5-7 syncs drain.
			backoff = time.Duration(opInProgressLinearBackoffS) * time.Second
		} else {
			// Exponential backoff (existing behavior): 1s, 2s, 4s
			backoff = time.Duration(1<<uint(attempt)) * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return lastErr
}
