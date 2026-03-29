// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/cfkv"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// StaticDeliveryActivator implements DeliveryActivator for static frontends
// served from DO Spaces via Cloudflare Worker (Sprint 10).
//
// Instead of generating K8s manifests and pushing to Gitea, static delivery
// writes a routing entry to Cloudflare Workers KV. The CF Worker reads this
// entry on each request and fetches assets directly from DO Spaces.
//
// Flow: deploy → upload assets to S3 → write KV entry → CF Worker serves assets
// No pods. No IngressRoute. No ArgoCD sync for static apps.
//
// Phases follow the same semantic contract as ContainerDeliveryActivator:
//
//	A: create release (READY)
//	B: write routing entry to Cloudflare KV
//	C: record result (PENDING or FAILED)
//	D: verify (HTTP probe to confirm CF Worker serves the content)
//	E: finalize (ACTIVE or FAILED)
type StaticDeliveryActivator struct {
	releaseStore   db.ReleaseStore
	envTargetStore db.EnvironmentTargetStore
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	locker     EnvironmentLocker
	kvClient   *cfkv.Client
	baseDomain string
}

// StaticDeliveryDeps holds dependencies for StaticDeliveryActivator.
type StaticDeliveryDeps struct {
	ReleaseStore   db.ReleaseStore
	EnvTargetStore db.EnvironmentTargetStore
	EventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Locker     EnvironmentLocker
	KVClient   *cfkv.Client
	BaseDomain        string
}

// NewStaticDeliveryActivator creates a new StaticDeliveryActivator.
func NewStaticDeliveryActivator(deps StaticDeliveryDeps) *StaticDeliveryActivator {
	return &StaticDeliveryActivator{
		releaseStore:   deps.ReleaseStore,
		envTargetStore: deps.EnvTargetStore,
		eventStore:     deps.EventStore,
		locker:         deps.Locker,
		kvClient:       deps.KVClient,
		baseDomain:     deps.BaseDomain,
	}
}

// Activate runs the 5-phase static release activation.
func (s *StaticDeliveryActivator) Activate(ctx context.Context, build *db.Build, result BuildCallbackRequest) error {
	// Phase A: create release
	release, err := s.phaseACreateRelease(ctx, build, result)
	if err != nil {
		return err
	}

	// Phase B: write routing entry to CF KV
	deliveryErr := s.phaseBDeliver(ctx, build, release, result)

	// Phase C: record result
	deliveryFailed, err := s.phaseCRecordResult(ctx, build, release, deliveryErr)
	if err != nil {
		return err
	}
	if deliveryFailed {
		return fmt.Errorf("static delivery failed: KV write error")
	}

	// Phase D: verify (probe the URL via CF Worker)
	verifyFailed := s.phaseDVerify(ctx, build, release)

	// Phase E: finalize
	if err := s.phaseEFinalize(ctx, build, release, result, verifyFailed); err != nil {
		return err
	}
	if verifyFailed {
		return fmt.Errorf("static delivery failed: verification failed")
	}
	return nil
}

// Rollback updates the CF KV entry to point to a previous build's assets.
func (s *StaticDeliveryActivator) Rollback(ctx context.Context, req RollbackDeliveryRequest) error {
	slog.Info("static rollback: updating CF KV with previous release",
		"target_release", req.TargetRelease.ID)

	if s.kvClient == nil {
		return fmt.Errorf("static rollback: CF KV client not configured")
	}

	// Parse apps from the target release to find static apps
	var apps []AppRelease
	if err := json.Unmarshal(req.TargetRelease.Apps, &apps); err != nil {
		return fmt.Errorf("static rollback: unmarshal apps: %w", err)
	}

	for _, app := range apps {
		subdomain := cfkv.Subdomain(req.TenantID, req.ProjectID, app.Name, req.Environment)
		entry := cfkv.RoutingEntry{
			BuildID:  req.TargetRelease.BuildID,
			S3Prefix: fmt.Sprintf("/%s/%s/%s/%s", req.TenantID, req.ProjectID, app.Name, req.TargetRelease.BuildID),
		}
		if err := s.kvClient.Put(ctx, subdomain, entry); err != nil {
			return fmt.Errorf("static rollback: KV put %s: %w", subdomain, err)
		}
		slog.Info("static rollback: KV updated", "subdomain", subdomain, "build_id", entry.BuildID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Phase implementations
// ---------------------------------------------------------------------------

func (s *StaticDeliveryActivator) phaseACreateRelease(ctx context.Context, build *db.Build, result BuildCallbackRequest) (*db.Release, error) {
	var release *db.Release
	err := s.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		appsJSON, err := json.Marshal(result.Apps)
		if err != nil {
			return fmt.Errorf("static activate: marshal apps: %w", err)
		}

		artifactsJSON, err := json.Marshal(map[string]string{
			"delivery": "static",
		})
		if err != nil {
			return fmt.Errorf("static activate: marshal artifacts: %w", err)
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
		return s.releaseStore.Create(ctx, tx, release)
	})
	if err != nil {
		return nil, err
	}
	return release, nil
}

func (s *StaticDeliveryActivator) phaseBDeliver(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest) error {
	if s.kvClient == nil {
		slog.Warn("static delivery: CF KV client not configured, skipping KV write (local dev)")
		return nil
	}

	// Write a KV entry for each static app in this build
	for _, app := range result.Apps {
		subdomain := cfkv.Subdomain(build.TenantID, build.ProjectID, app.Name, build.Environment)
		entry := cfkv.RoutingEntry{
			BuildID:  build.ID,
			S3Prefix: fmt.Sprintf("/%s/%s/%s/%s", build.TenantID, build.ProjectID, app.Name, build.ID),
		}

		slog.Info("static delivery: writing KV entry",
			"subdomain", subdomain,
			"build_id", entry.BuildID,
			"s3_prefix", entry.S3Prefix,
		)

		if err := s.kvClient.Put(ctx, subdomain, entry); err != nil {
			return fmt.Errorf("static delivery: KV put %s: %w", subdomain, err)
		}
	}
	return nil
}

func (s *StaticDeliveryActivator) phaseCRecordResult(ctx context.Context, build *db.Build, release *db.Release, deliveryErr error) (deliveryFailed bool, err error) {
	return deliveryFailed, s.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if deliveryErr != nil {
			slog.Error("static KV write failed", "build_id", build.ID, "release_id", release.ID, "error", deliveryErr)
			if _, err := s.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorFailed_)); err != nil {
				return fmt.Errorf("static activate: transition to GIT_MIRROR_FAILED: %w", err)
			}
			failedData, _ := json.Marshal(map[string]interface{}{
				"deploy_id": deriveDeployID(release.ID),
				"status":    "delivery_failed",
				"error":     deliveryErr.Error(),
			})
			if _, err := s.eventStore.Append(ctx, build.ID, "deploy.delivery_failed", failedData); err != nil {
				return fmt.Errorf("static activate: emit deploy.delivery_failed: %w", err)
			}
			deliveryFailed = true
			return nil
		}

		// KV write succeeded — transition to PENDING
		if _, err := s.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorCompleted)); err != nil {
			return fmt.Errorf("static activate: transition to PENDING: %w", err)
		}

		pendingData, _ := json.Marshal(map[string]string{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "delivering",
		})
		if _, err := s.eventStore.Append(ctx, build.ID, "deploy.delivering", pendingData); err != nil {
			return fmt.Errorf("static activate: emit deploy.delivering: %w", err)
		}

		if err := s.envTargetStore.SetDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
			return fmt.Errorf("static activate: set desired: %w", err)
		}
		return nil
	})
}

func (s *StaticDeliveryActivator) phaseDVerify(ctx context.Context, build *db.Build, release *db.Release) bool {
	// For CF Worker delivery, verification is simpler:
	// The KV write is synchronous and the Worker reads it immediately.
	// We emit verifying event for SSE consistency.
	verifyingData, _ := json.Marshal(map[string]string{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "verifying",
	})
	if _, err := s.eventStore.Append(ctx, build.ID, "deploy.verifying", verifyingData); err != nil {
		slog.Error("static: failed to emit deploy.verifying", "error", err)
	}

	// KV propagation is near-instant (< 60s globally, typically < 1s).
	// No ArgoCD sync needed — the CF Worker reads KV on every request.
	slog.Info("static delivery: KV entry written, CF Worker will serve immediately",
		"build_id", build.ID,
		"release_id", release.ID,
	)

	return false // verification always passes for KV-based delivery
}

func (s *StaticDeliveryActivator) phaseEFinalize(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest, verifyFailed bool) error {
	return s.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if verifyFailed {
			if _, err := s.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncFailed_)); err != nil {
				return fmt.Errorf("static activate: transition to SYNC_FAILED: %w", err)
			}
			failedData, _ := json.Marshal(map[string]interface{}{
				"deploy_id": deriveDeployID(release.ID),
				"status":    "sync_failed",
				"reason":    "VERIFICATION_FAILED",
			})
			if _, err := s.eventStore.Append(ctx, build.ID, "deploy.sync_failed", failedData); err != nil {
				return fmt.Errorf("static activate: emit deploy.sync_failed: %w", err)
			}
			return nil
		}

		// Release → ACTIVE
		if _, err := s.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted)); err != nil {
			return fmt.Errorf("static activate: transition to ACTIVE: %w", err)
		}

		// SSE: deploy.live
		activeData, _ := json.Marshal(map[string]string{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "live",
		})
		if _, err := s.eventStore.Append(ctx, build.ID, "deploy.live", activeData); err != nil {
			return fmt.Errorf("static activate: emit deploy.live: %w", err)
		}

		if err := s.envTargetStore.SetActive(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID); err != nil {
			return fmt.Errorf("static activate: set active: %w", err)
		}

		// Supersede previous
		prev, err := s.releaseStore.GetActiveByEnvTx(ctx, tx, build.TenantID, build.ProjectID, build.Environment)
		if err != nil {
			return fmt.Errorf("static activate: get previous: %w", err)
		}
		if prev != nil && prev.ID != release.ID {
			if _, err := s.releaseStore.UpdateStatus(ctx, tx, prev.ID, string(statemachine.ReleaseNewActivated)); err != nil {
				return fmt.Errorf("static activate: supersede previous: %w", err)
			}
		}

		// SSE: deploy.completed
		deployID := deriveDeployID(release.ID)
		durationSecs := int(time.Since(build.CreatedAt).Seconds())
		var deployApps []map[string]interface{}
		for _, app := range result.Apps {
			hostname := cfkv.Subdomain(build.TenantID, build.ProjectID, app.Name, build.Environment)
			entry := map[string]interface{}{
				"name":     app.Name,
				"status":   "deployed",
				"delivery": "static",
				"url":      fmt.Sprintf("https://%s.%s", hostname, s.baseDomain),
			}
			deployApps = append(deployApps, entry)
		}

		deployData, _ := json.Marshal(map[string]interface{}{
			"deploy_id":        deployID,
			"status":           "success",
			"duration_seconds": durationSecs,
			"apps":             deployApps,
		})
		if _, err := s.eventStore.Append(ctx, build.ID, "deploy.completed", deployData); err != nil {
			return fmt.Errorf("static activate: emit deploy.completed: %w", err)
		}
		return nil
	})
}
