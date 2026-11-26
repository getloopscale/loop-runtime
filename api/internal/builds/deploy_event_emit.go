// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

// stageDeployFailed stages a deploy.failed event on the transactional outbox
// (deploy_events, engine migration 000051) on the SAME tx as the build→FAILED
// transition, so failure-event capture is atomic with the deploy (Option C,
// MEET-2026-07-03-005 + MEET-2026-07-04-001). loop-console drains + fans out to
// the customer's workspace_webhooks; the engine never references the webhook
// tables (decoupling invariant).
//
// Nil-safe: a nil store (unit paths not exercising the outbox) is a no-op,
// mirroring the deploy.succeeded emit's onReleaseActivated convention.
//
// FATAL by design at the call sites that check the error: if the event can't be
// staged the whole FAILED transition rolls back and the caller retries — this is
// what makes failure delivery at-least-once (no silently-lost deploy.failed).
//
// Emitted ONLY from the three build-EXECUTION-failure paths (build_orchestrator_
// complete, build_reconcile_retry, build_reaper). The submit-failure path
// (failQueuedBuild) deliberately does NOT emit — a workflow-submission failure
// is a platform infrastructure error (the build never ran), a distinct class
// from deploy.failed, and its lock-free CAS is intentional per ADR
// 2026-07-03-m9-5b (see the doc-comment there). Per MEET-2026-07-04-001.
//
// Payload carries customer-safe fields only (Rule 3) — no internal terms.
func stageDeployFailed(ctx context.Context, tx db.DBTX, deployEventStore store.DeployEventStore, b *db.Build) error {
	if deployEventStore == nil || b == nil {
		return nil
	}
	deployID := deriveBuildDeployID(b.ID)
	payload := map[string]any{
		"event":       "deploy.failed",
		"deploy_id":   deployID,
		"project":     b.ProjectID,
		"environment": b.Environment,
		"status":      "failed",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	}
	if b.CommitSHA != "" {
		payload["commit"] = b.CommitSHA
	}
	return deployEventStore.AppendDeployEvent(ctx, tx, store.DeployEvent{
		// Fresh UUID per emit (a build can be re-driven to FAILED after a retry;
		// each is a distinct event) — never the build id, mirroring #176.
		TransitionID:  "dpx-" + uuid.NewString(),
		EventType:     "deploy.failed",
		TenantID:      b.TenantID,
		ProjectID:     b.ProjectID,
		DeployID:      deployID,
		CorrelationID: b.CorrelationID,
		Payload:       payload,
	})
}

// deriveBuildDeployID mirrors the release deriveDeployID shape ("d-{8hex}") for
// a build id ("bld-{16hex}"), so a failed build and its (never-created) release
// share the customer-facing id space.
func deriveBuildDeployID(buildID string) string {
	hex := strings.TrimPrefix(buildID, "bld-")
	if len(hex) >= 8 {
		return "d-" + hex[:8]
	}
	return "d-" + hex
}
