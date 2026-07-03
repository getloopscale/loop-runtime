// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package release is the release-activation bounded context (activate, converge, promote, rollback, reap). M9 Step 6.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/shared"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OnReleaseActivated is an optional callback invoked AFTER a release transitions to ACTIVE.
// Used by the GitHub Status Notifier to update commit status to "success".
// Registered once at startup via RegisterReleaseActivatedCallback().
// Non-blocking: runs in a goroutine. Errors are logged, not propagated.
// OnReleaseActivated callback receives buildID + environment after a release goes ACTIVE.
// The callback is responsible for looking up the build's source commit SHA (not the gitops SHA).
var onReleaseActivated func(ctx context.Context, tenantID, projectName, buildID, environment string)

// RegisterReleaseActivatedCallback sets the global callback for release activation.
func RegisterReleaseActivatedCallback(fn func(ctx context.Context, tenantID, projectName, buildID, environment string)) {
	onReleaseActivated = fn
}

// OnBuildFailed is an optional callback invoked when a build transitions to FAILED.
// Used by the GitHub Status Notifier to update commit status to "failure".
var onBuildFailed func(ctx context.Context, tenantID, projectName, commitSHA, buildID string)

// RegisterBuildFailedCallback sets the global callback for build failure.
func RegisterBuildFailedCallback(fn func(ctx context.Context, tenantID, projectName, commitSHA, buildID string)) {
	onBuildFailed = fn
}

// NotifyBuildFailed asynchronously invokes the registered build-failed callback,
// if one is set. Release owns the callback's delivery semantics: the callback
// runs in a goroutine under a context that preserves request-scoped values
// (correlation ID, OTel span) via context.WithoutCancel but is decoupled from
// the inbound request's cancellation, capped at 30s. When no callback is
// registered this is a no-op. M9 Step 6: the build context calls this instead of
// peeking at the private onBuildFailed var, keeping the callback encapsulated in
// the release context that owns it (find_code_004 / CODE-004 delivery contract
// preserved byte-for-byte).
func NotifyBuildFailed(ctx context.Context, tenantID, projectName, commitSHA, buildID string) {
	if onBuildFailed == nil {
		return
	}
	parentCtx := context.WithoutCancel(ctx)
	go func() {
		cbCtx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
		defer cancel()
		onBuildFailed(cbCtx, tenantID, projectName, commitSHA, buildID)
	}()
}


// ReleaseActivationStore defines the minimal store interface needed by the
// release activation kernel. Both db.ReleaseStore and ReleaseConvergenceStore
// satisfy this interface.
type ReleaseActivationStore interface {
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
	GetActiveByEnvTx(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (*db.Release, error)
	// GetByIDTx re-fetches a release inside the lock transaction.
	// MEET-2026-03-31-023 T0-1: prevents duplicate activation race.
	GetByIDTx(ctx context.Context, tx db.DBTX, id string) (*db.Release, error)
}

// ReleaseActivationEventStore is the subset of EventStore used by activation.
type ReleaseActivationEventStore interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
}

// ActivationAppInfo describes one app in the deploy.completed SSE event.
type ActivationAppInfo struct {
	Name   string
	Status string
	URL    string // optional; set by container delivery (has build callback URLs)
}

// ActivateReleaseKernel implements the correct release activation sequence.
// CODE-0001: Queries the previous active release BEFORE transitioning the new
// release to ACTIVE, preventing the race where GetActiveByEnvTx returns the
// just-activated release instead of the previous one.
//
// This is the single source of truth for release activation. All three activation
// paths (ContainerDeliveryActivator, ReleaseConvergenceLoop, SyncCallbackActivator)
// MUST call this function inside a withEnvironmentLock transaction.
//
// Sequence: (1) get prev release, (2) transition ACTIVE, (3) emit deploy.live,
// (4) SetActive, (5) supersede prev, (6) emit deploy.completed.
func ActivateReleaseKernel(
	ctx context.Context,
	tx db.DBTX,
	release *db.Release,
	releaseStore ReleaseActivationStore,
	envTargetStore store.EnvironmentTargetStore,
	eventStore ReleaseActivationEventStore,
	apps []ActivationAppInfo,
	durationSecs int,
	logger *slog.Logger,
) error {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.release.activate",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", release.TenantID),
			attribute.String("loopzero.environment", release.Environment),
			attribute.String("loopzero.release_id", release.ID),
		))
	defer span.End()

	// MEET-2026-03-31-023 T0-1: Idempotency guard — re-fetch release inside the lock
	// to prevent duplicate activation when SyncCallbackActivator and
	// ReleaseConvergenceLoop race on the same release.
	current, err := releaseStore.GetByIDTx(ctx, tx, release.ID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("activate release: re-fetch inside lock: %w", err)
	}
	if current.Status != string(statemachine.RPhPending) {
		logger.Info("activate release: already transitioned, skipping",
			"release_id", release.ID, "current_status", current.Status)
		return nil
	}

	// Step 1: CODE-0001 — query previous ACTIVE release BEFORE activating new one.
	prevRelease, err := releaseStore.GetActiveByEnvTx(ctx, tx, release.TenantID, release.ProjectID, release.Environment)
	if err != nil {
		return fmt.Errorf("activate release: get previous active: %w", err)
	}

	// Step 2: Supersede previous active release BEFORE activating the new one.
	// T5.1 CV-M-1 dogfood regression fix (2026-06-02): partial unique index
	// `idx_releases_active_unique` (migration 000022) enforces at-most-one-
	// ACTIVE-per-env at the DB layer. Pre-fix order (UPDATE new ACTIVE first,
	// then UPDATE prev SUPERSEDED) hit SQLSTATE 23505 because for an instant
	// inside the tx, two rows were ACTIVE for the same (tenant, project, env)
	// — which is precisely what the index now forbids. By superseding the
	// previous release FIRST, we never have two ACTIVE rows simultaneously.
	// Both UPDATEs run inside the same tx (withEnvironmentLock), so they
	// remain atomic from any external observer's perspective: either both
	// succeed (prev SUPERSEDED + new ACTIVE) or both rollback (prev stays
	// ACTIVE + new stays PENDING).
	//
	// State machine validity:
	//   - prev: ACTIVE --(RPEventNewActivated)--> SUPERSEDED  ✓
	//   - new:  PENDING --(RPEventSyncCompleted)--> ACTIVE   ✓
	// Both transitions are independent — order swap is safe.
	//
	// LIVE-caught regression: Journey 06 Rollback "v2 release is ACTIVE"
	// timed out at 6m because convergence loop retried indefinitely with
	// CONCURRENT_ACTIVATION errors until release reaper killed at sync
	// timeout. Pre-T5.1 partial index, this bug was silent (2 ACTIVE rows
	// coexisted briefly); post-T5.1 the index surfaces it, and the
	// CONCURRENT_ACTIVATION typed error (CV-M-1, commit d8dbeaf4) made
	// the loop fail-fast instead of silently retrying. The proper fix is
	// at the activation kernel: invert the order.
	if prevRelease != nil && prevRelease.ID != release.ID {
		if _, supersedeErr := releaseStore.UpdateStatus(ctx, tx, prevRelease.ID, string(statemachine.RPEventNewActivated)); supersedeErr != nil {
			logger.Error("activate release: failed to supersede previous",
				"prev_id", prevRelease.ID, "new_id", release.ID, "error", supersedeErr)
			return fmt.Errorf("activate release: supersede previous %s: %w", prevRelease.ID, supersedeErr)
		}
		logger.Info("activate release: superseded previous",
			"prev_id", prevRelease.ID, "new_id", release.ID)
	}

	// Step 3: Transition PENDING → ACTIVE. With the previous release now
	// SUPERSEDED (Step 2), the partial unique index allows this transition
	// without violating the at-most-one-ACTIVE-per-env invariant.
	if _, err := releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.RPEventSyncCompleted)); err != nil {
		return fmt.Errorf("activate release: transition to ACTIVE: %w", err)
	}

	// MEET-2026-03-31-023 T1-8: Record convergence duration (PENDING → ACTIVE).
	convergenceDuration := time.Since(release.CreatedAt).Seconds()
	metrics.ReleaseConvergenceDuration.WithLabelValues(release.TenantID, release.Environment).Observe(convergenceDuration)

	// End-to-end deploy duration: build creation → release ACTIVE.
	// durationSecs is computed by callers as time.Since(build.CreatedAt).
	metrics.DeployDuration.WithLabelValues(release.TenantID, release.Environment).Observe(float64(durationSecs))

	// Phase 4A: Deploy lead time — same value, separate histogram for executive SLO tracking.
	metrics.DeployLeadTime.WithLabelValues(release.TenantID, release.Environment).Observe(float64(durationSecs))

	// Step 4: Emit deploy.live SSE event.
	liveData := shared.SafeJSONMarshal(map[string]interface{}{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "live",
	})
	if _, err := eventStore.Append(ctx, release.BuildID, "deploy.live", liveData); err != nil {
		logger.Error("activate release: failed to emit deploy.live", "release_id", release.ID, "error", err)
	}

	// Step 5: Update environment target pointer.
	if err := envTargetStore.SetActive(ctx, tx, release.TenantID, release.ProjectID, release.Environment, release.ID); err != nil {
		return fmt.Errorf("activate release: set active: %w", err)
	}

	// Step 6: Emit deploy.completed SSE event — CLI waits for this to exit.
	deployID := deriveDeployID(release.ID)
	appResults := make([]map[string]interface{}, 0, len(apps))
	for _, app := range apps {
		entry := map[string]interface{}{
			"name":   app.Name,
			"status": app.Status,
		}
		if app.URL != "" {
			entry["url"] = app.URL
		}
		appResults = append(appResults, entry)
	}
	completedData := shared.SafeJSONMarshal(map[string]interface{}{
		"deploy_id":        deployID,
		"status":           "success",
		"duration_seconds": durationSecs,
		"apps":             appResults,
	})
	// T5.6 of deep-review-2026-05-10-remediation-plan (find_event_completed_swallowed MEDIUM):
	// `deploy.completed` is the event the CLI waits on — a single emit
	// failure means the CLI hangs until the deploy timeout. 3 attempts
	// with 100ms backoff covers the typical "transient lock contention"
	// failure mode without adding meaningful tail latency.
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := eventStore.Append(ctx, release.BuildID, "deploy.completed", completedData)
		if err == nil {
			break
		}
		logger.Error("activate release: failed to emit deploy.completed",
			"release_id", release.ID, "attempt", attempt, "error", err)
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	// Step 7: Notify external systems (GitHub commit status, etc.)
	// T6.4 of deep-review-remediation-plan (closes find_code_004 / CODE-004 MEDIUM).
	// context.WithoutCancel preserves trace + correlation ID values but
	// detaches cancellation so the callback survives request close.
	if onReleaseActivated != nil {
		parentCtx := context.WithoutCancel(ctx)
		go func() {
			cbCtx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
			defer cancel()
			onReleaseActivated(cbCtx, release.TenantID, release.ProjectID, release.BuildID, release.Environment)
		}()
	}

	return nil
}

// releaseAppsToActivationApps converts the JSON-encoded apps from a release
// into ActivationAppInfo structs for the activation kernel.
// F-03 fix: computes URL for server/frontend apps so deploy.completed SSE event includes it.
//
// Plan release-blockers-2026-06-25-set v1.1 #61 (2026-06-26):
// Closes loopzero-cloud#61 ("loopzero deploy announces .loopzero.local URL not publicly
// resolvable"). The deploy.completed SSE event URL composition is now the
// SAME canonical that status.go uses — operator-configured LOOPZERO_PUBLIC_DOMAIN
// (e.g. `prod-dev.loopzero.example`) preferred when set; falls back to BASE_DOMAIN
// (legacy default `loopzero.local`) only when no public override is wired. This
// mirrors the existing `StatusHandler.publicDomain` fallback ladder so
// `loopzero deploy` and `loopzero status` emit byte-identical URLs (no more
// "deploy says .loopzero.local, status says .prod-dev.loopzero.example" mismatch).
func releaseAppsToActivationApps(release *db.Release, baseDomain, publicDomain string) []ActivationAppInfo {
	var specs []db.BuildAppSpec
	if release.Apps != nil {
		if err := json.Unmarshal(release.Apps, &specs); err != nil {
			slog.Warn("failed to unmarshal release apps for activation",
				"release_id", release.ID, "error", err)
		}
	}
	// Canonical: prefer the operator-configured public domain so the URL
	// announced by `loopzero deploy` is the SAME URL `loopzero status` returns
	// AND publicly resolvable. Empty publicDomain → fall back to baseDomain
	// (preserves the pre-#61 behavior for envs without a public wildcard).
	domain := publicDomain
	if domain == "" {
		domain = baseDomain
	}
	apps := make([]ActivationAppInfo, 0, len(specs))
	for _, s := range specs {
		url := ""
		if s.Type == "server" && domain != "" {
			url = fmt.Sprintf("https://%s-%s-%s-%s.%s",
				release.TenantID, release.ProjectID, s.Name, release.Environment, domain)
		}
		apps = append(apps, ActivationAppInfo{Name: s.Name, Status: "deployed", URL: url})
	}
	return apps
}

// deriveDeployID creates a deploy_id in the format d-{8hex} from a release ID.
// MOD-5: Increased from 4 to 8 hex chars (32 bits) to reduce collision probability.
// With 4 hex chars (16 bits), collisions were guaranteed after ~256 deploys.
// With 8 hex chars (32 bits), birthday paradox gives 50% collision at ~65K deploys.
// Example: rel-8f3a2b1c → d-8f3a2b1c
func deriveDeployID(releaseID string) string {
	hex := strings.TrimPrefix(releaseID, "rel-")
	if len(hex) >= 8 {
		return "d-" + hex[:8]
	}
	return "d-" + hex
}

// RetryRelease retries the GitOps push for a release stuck in GIT_MIRROR_FAILED.
//
