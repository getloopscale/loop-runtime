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

	"github.com/google/uuid"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/events"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/naming"
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

// ActivationKernel implements the correct release activation sequence. It is
// the parameter object the three activation paths (ContainerDeliveryActivator,
// ReleaseConvergenceLoop, SyncCallbackActivator) assemble from the stores they
// already hold, instead of each passing the same four stores and a logger.
//
// This is the single source of truth for release activation. Every path MUST
// call Activate inside a withEnvironmentLock transaction.
type ActivationKernel struct {
	Releases   ReleaseActivationStore
	EnvTargets store.EnvironmentTargetStore
	Events     ReleaseActivationEventStore
	// DeployEvents is the transactional outbox. Optional: nil in unit paths
	// that do not exercise it, in which case no deploy.succeeded is staged.
	DeployEvents store.DeployEventStore
	Logger       *slog.Logger
}

// Activate moves a PENDING release to ACTIVE.
//
// CODE-0001: the previous active release is queried BEFORE the new release is
// transitioned, so GetActiveByEnvTx never returns the just-activated release.
//
// Sequence: (0) re-fetch inside the lock, (1) get previous release,
// (2) supersede it, (3) transition ACTIVE, (4) emit deploy.live,
// (5) SetActive, (5.5) stage the outbox event, (6) emit deploy.completed,
// (7) notify external systems.
func (k ActivationKernel) Activate(
	ctx context.Context, tx db.DBTX, release *db.Release, apps []ActivationAppInfo, durationSecs int,
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
	current, err := k.Releases.GetByIDTx(ctx, tx, release.ID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("activate release: re-fetch inside lock: %w", err)
	}
	if current.Status != string(statemachine.RPhPending) {
		k.Logger.Info("activate release: already transitioned, skipping",
			"release_id", release.ID, "current_status", current.Status)
		return nil
	}

	if err := k.supersedePrevious(ctx, tx, release); err != nil {
		return err
	}

	// Step 3: Transition PENDING → ACTIVE. With the previous release now
	// SUPERSEDED, the partial unique index allows this transition without
	// violating the at-most-one-ACTIVE-per-env invariant.
	if _, err := k.Releases.UpdateStatus(ctx, tx, release.ID, string(statemachine.RPEventSyncCompleted)); err != nil {
		return fmt.Errorf("activate release: transition to ACTIVE: %w", err)
	}
	recordActivationMetrics(release, durationSecs)

	// Step 4: Emit deploy.live SSE event.
	liveData := events.SafeJSONMarshal(map[string]interface{}{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "live",
	})
	if _, err := k.Events.Append(ctx, release.BuildID, "deploy.live", liveData); err != nil {
		k.Logger.Error("activate release: failed to emit deploy.live", "release_id", release.ID, "error", err)
	}

	// Step 5: Update environment target pointer.
	if err := k.EnvTargets.SetActive(ctx, tx, release.TenantID, release.ProjectID, release.Environment, release.ID); err != nil {
		return fmt.Errorf("activate release: set active: %w", err)
	}

	if err := k.stageDeploySucceeded(ctx, tx, release); err != nil {
		return err
	}
	if err := k.emitCompleted(ctx, release, apps, durationSecs); err != nil {
		return err
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

// supersedePrevious runs steps 1 and 2: find the environment's ACTIVE release
// and supersede it BEFORE the new one is activated.
//
// T5.1 CV-M-1 dogfood regression fix (2026-06-02): the partial unique index
// `idx_releases_active_unique` (migration 000022) enforces at-most-one-ACTIVE
// per env. Activating first and superseding second hit SQLSTATE 23505 because
// two rows were ACTIVE for an instant inside the tx. Both UPDATEs run in the
// same tx (withEnvironmentLock), so they stay atomic to any observer.
//
// State machine validity:
//   - prev: ACTIVE --(RPEventNewActivated)--> SUPERSEDED
//   - new:  PENDING --(RPEventSyncCompleted)--> ACTIVE
func (k ActivationKernel) supersedePrevious(ctx context.Context, tx db.DBTX, release *db.Release) error {
	prevRelease, err := k.Releases.GetActiveByEnvTx(ctx, tx, release.TenantID, release.ProjectID, release.Environment)
	if err != nil {
		return fmt.Errorf("activate release: get previous active: %w", err)
	}
	if prevRelease == nil || prevRelease.ID == release.ID {
		return nil
	}
	if _, err := k.Releases.UpdateStatus(ctx, tx, prevRelease.ID, string(statemachine.RPEventNewActivated)); err != nil {
		k.Logger.Error("activate release: failed to supersede previous",
			"prev_id", prevRelease.ID, "new_id", release.ID, "error", err)
		return fmt.Errorf("activate release: supersede previous %s: %w", prevRelease.ID, err)
	}
	k.Logger.Info("activate release: superseded previous",
		"prev_id", prevRelease.ID, "new_id", release.ID)
	return nil
}

// recordActivationMetrics observes convergence (PENDING → ACTIVE, T1-8) and the
// end-to-end deploy duration, which callers compute as time.Since(build.CreatedAt).
// Deploy lead time is the same value in a separate histogram for SLO tracking.
func recordActivationMetrics(release *db.Release, durationSecs int) {
	metrics.ReleaseConvergenceDuration.WithLabelValues(release.TenantID, release.Environment).
		Observe(time.Since(release.CreatedAt).Seconds())
	metrics.DeployDuration.WithLabelValues(release.TenantID, release.Environment).Observe(float64(durationSecs))
	metrics.DeployLeadTime.WithLabelValues(release.TenantID, release.Environment).Observe(float64(durationSecs))
}

// stageDeploySucceeded is step 5.5, the transactional outbox (Option C,
// MEET-2026-07-03-005): the deploy.succeeded event is staged on the SAME tx.
// loopzero-cloud drains deploy_events and fans out to the customer's webhooks; the
// engine never references the webhook tables. FATAL by design: if the event
// cannot be staged the activation rolls back and the convergence loop retries,
// which is what makes delivery at-least-once. transition_id is a fresh UUID per
// ENTRY into ACTIVE (a release re-enters ACTIVE after rollback recovery). The
// payload carries customer-safe fields only (Rule 3).
func (k ActivationKernel) stageDeploySucceeded(ctx context.Context, tx db.DBTX, release *db.Release) error {
	if k.DeployEvents == nil {
		return nil
	}
	deployID := deriveDeployID(release.ID)
	if err := k.DeployEvents.AppendDeployEvent(ctx, tx, store.DeployEvent{
		TransitionID:  "dpx-" + uuid.NewString(),
		EventType:     "deploy.succeeded",
		TenantID:      release.TenantID,
		ProjectID:     release.ProjectID,
		DeployID:      deployID,
		CorrelationID: release.CorrelationID,
		Payload: map[string]any{
			"event":       "deploy.succeeded",
			"deploy_id":   deployID,
			"project":     release.ProjectID,
			"environment": release.Environment,
			"status":      "success",
			"occurred_at": time.Now().UTC().Format(time.RFC3339),
		},
	}); err != nil {
		return fmt.Errorf("activate release: stage deploy event: %w", err)
	}
	return nil
}

// emitCompleted is step 6: the deploy.completed SSE event the CLI waits on.
// T5.6 (find_event_completed_swallowed): a single emit failure would leave the
// CLI hanging until the deploy timeout, so it is attempted 3 times with a
// 100ms backoff. Exhausting the attempts is logged, not returned — the release
// is already ACTIVE.
func (k ActivationKernel) emitCompleted(ctx context.Context, release *db.Release, apps []ActivationAppInfo, durationSecs int) error {
	appResults := make([]map[string]interface{}, 0, len(apps))
	for _, app := range apps {
		entry := map[string]interface{}{"name": app.Name, "status": app.Status}
		if app.URL != "" {
			entry["url"] = app.URL
		}
		appResults = append(appResults, entry)
	}
	completedData := events.SafeJSONMarshal(map[string]interface{}{
		"deploy_id":        deriveDeployID(release.ID),
		"status":           "success",
		"duration_seconds": durationSecs,
		"apps":             appResults,
	})
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := k.Events.Append(ctx, release.BuildID, "deploy.completed", completedData)
		if err == nil {
			return nil
		}
		k.Logger.Error("activate release: failed to emit deploy.completed",
			"release_id", release.ID, "attempt", attempt, "error", err)
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
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
	domain := userFacingDomain(publicDomain, baseDomain)
	apps := make([]ActivationAppInfo, 0, len(specs))
	for _, s := range specs {
		url := ""
		if s.Type == "server" {
			url = naming.AppURL(release.TenantID, release.ProjectID, s.Name, release.Environment, domain)
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
