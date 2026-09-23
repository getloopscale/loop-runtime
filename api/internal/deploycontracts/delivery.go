// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import (
	"context"
	"errors"
	"time"
)

// ErrActivationAlreadyHandled is a sentinel error returned by DeliveryActivator.Activate
// when the activation failure has already been recorded (SSE event emitted, release state
// transitioned). Callers should NOT emit additional failure events.
// BRC-002 fix: prevents duplicate deploy.sync_failed SSE events.
var ErrActivationAlreadyHandled = errors.New("activation failure already handled by delivery activator")

// ActivationBuild is the completed build handed to delivery, reduced to the
// fields the activation contract actually reads.
//
// It replaced `*db.Build` on 2026-09-04 (B-079). The persistence row carries 16
// fields (reflect, 2026-09-04); activation reads these 7, and the other 9 —
// status, workflow id, bundle digest, the timestamps of the build's own
// lifecycle — are the build context's business, not the delivery contract's.
// Passing the row made every package that imports this sink import persistence
// to get at six strings and a timestamp.
//
// It is a VALUE, not a pointer: there is no nil delivery target, and the
// activator dereferenced the old pointer in five places without checking.
type ActivationBuild struct {
	// ID keys the SSE event stream the CLI tails during a deploy.
	ID string
	// TenantID, ProjectID and Environment are the environment-lock scope and
	// the ArgoCD application identity (naming.ArgoAppName).
	TenantID    string
	ProjectID   string
	Environment string
	// CreatedBy and CorrelationID are persisted onto the release row so the
	// deploy stays attributable end to end.
	CreatedBy     string
	CorrelationID string
	// CreatedAt is the origin of the deploy duration reported on completion.
	CreatedAt time.Time
}

// DeliveryActivator abstracts the release activation strategy (MEET-2026-03-26-009).
//
// Both container and static delivery follow the same 5-phase semantic contract:
//   - Phase A: create release (READY)
//   - Phase B: deliver (push manifests to Gitea OR upload assets to S3)
//   - Phase C: record result (PENDING or delivery-specific failure)
//   - Phase D: verify (ArgoCD sync + health check OR integrity probe)
//   - Phase E: finalize (ACTIVE or FAILED)
//
// SSE events are delivery-agnostic: deploy.delivering, deploy.verifying, deploy.live.
// Implementations MUST emit these same events — diverging means two products, not one.
//
// The BuildOrchestrator dispatches to the correct activator based on build.DeliveryModel.
// Zero if/else branches in the orchestrator — polymorphism via this interface.
type DeliveryActivator interface {
	// Activate runs the 5-phase release activation for a completed build.
	// Returns nil on success (release is ACTIVE). Returns error if any phase fails
	// (release may be in READY, PENDING, or a failure state depending on which phase failed).
	Activate(ctx context.Context, build ActivationBuild, result BuildCallbackRequest) error

	// Rollback delivers a previous release's artifacts to the runtime.
	// For containers: fetches old manifests from Gitea, pushes them, triggers ArgoCD sync.
	// For static: pushes IngressRoute with old release-id path.
	// State machine transitions (ACTIVE→ROLLED_BACK, SUPERSEDED→ACTIVE) are handled
	// by the RollbackOrchestrator, NOT by this method.
	Rollback(ctx context.Context, req RollbackDeliveryRequest) error
}

// RollbackTarget is the release being reactivated, reduced to what delivery
// reads from it.
//
// It replaced `*db.Release` on 2026-09-04 (B-079). Container rollback touches
// exactly two of the release row's 18 fields (reflect, 2026-09-04), and both are load-bearing: an
// empty GitCommit is the guard that refuses a rollback with no manifests to
// fetch, and ID attributes the pushed manifests to the release they came from.
type RollbackTarget struct {
	// ID of the release whose manifests are being re-delivered.
	ID string
	// GitCommit is the GitOps commit the manifests are read back from. Empty
	// means the release never reached the mirror and cannot be rolled back to.
	GitCommit string
}

// RollbackDeliveryRequest contains the delivery-specific inputs for rollback.
// State machine transitions are NOT part of this request — they are handled
// by the RollbackOrchestrator before and after calling DeliveryActivator.Rollback.
type RollbackDeliveryRequest struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string
	TargetRelease *RollbackTarget // the release to reactivate
	CorrelationID string
}
