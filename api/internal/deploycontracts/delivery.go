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

// ErrSyncApplicationNotFound is wrapped by SyncClient.TriggerSync when the
// ArgoCD Application does not exist (HTTP 404). It is the only signal delivery
// may read as "not created yet, retry later": classifying by the message text
// also matched transport errors such as "host not found".
var ErrSyncApplicationNotFound = errors.New("argocd application not found")

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

// DeliveryActivator is the port through which the build and rollback paths hand
// a release to the runtime (MEET-2026-03-26-009). Its one implementation is
// release.ContainerDeliveryActivator, constructed once at the composition root
// and shared by BuildOrchestrator and RollbackOrchestrator. Static delivery was
// removed (build_plan.go T3.3); the port stays because it is what keeps the
// build package independent of the release package's concrete type, and what
// lets the orchestrators be tested without GitOps and ArgoCD.
//
// Activation runs these phases:
//   - A: create the release (READY)
//   - B: push manifests to the GitOps repository
//   - C: record the result (PENDING, or GIT_MIRROR_FAILED)
//   - D: trigger the sync; verification is asynchronous (callback activator +
//     convergence loop) and moves the release to ACTIVE or SYNC_FAILED
//
// SSE events: deploy.delivering and deploy.verifying here; deploy.completed is
// emitted by whichever asynchronous path activates the release.
type DeliveryActivator interface {
	// Activate runs phases A-D for a completed build. It returns nil once the
	// release is PENDING and the sync was triggered; it returns an error if a
	// phase fails (the release may then be READY, PENDING or in a failure state,
	// depending on which phase failed).
	Activate(ctx context.Context, build ActivationBuild, result BuildCallbackRequest) error

	// Rollback re-delivers a previous release's manifests (read back from the
	// GitOps history), triggers the sync and returns only once the sync is
	// confirmed, so the caller may flip release state afterwards.
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
