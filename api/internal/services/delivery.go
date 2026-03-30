// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"

	"github.com/loopzero/loopzero/api/internal/db"
)

// ErrActivationAlreadyHandled is a sentinel error returned by DeliveryActivator.Activate
// when the activation failure has already been recorded (SSE event emitted, release state
// transitioned). Callers should NOT emit additional failure events.
// BRC-002 fix: prevents duplicate deploy.sync_failed SSE events.
var ErrActivationAlreadyHandled = errors.New("activation failure already handled by delivery activator")

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
	Activate(ctx context.Context, build *db.Build, result BuildCallbackRequest) error

	// Rollback delivers a previous release's artifacts to the runtime.
	// For containers: fetches old manifests from Gitea, pushes them, triggers ArgoCD sync.
	// For static: pushes IngressRoute with old release-id path, triggers ArgoCD sync.
	// State machine transitions (ACTIVE→ROLLED_BACK, SUPERSEDED→ACTIVE) are handled
	// by the RollbackOrchestrator, NOT by this method.
	Rollback(ctx context.Context, req RollbackDeliveryRequest) error
}

// DispatchingDeliveryActivator selects between container and static delivery
// based on whether any app in the build result has a static delivery model.
//
// ARCH-009 fix: Previously, the BuildOrchestrator always defaulted to
// ContainerDeliveryActivator because StaticDeliveryActivator was never wired
// in main.go. This dispatcher makes the selection explicit.
//
// NOTE: Full delivery model dispatch requires adding delivery_model to the
// builds table (migration). For now, the dispatcher checks the result apps.
type DispatchingDeliveryActivator struct {
	Container DeliveryActivator
	Static    DeliveryActivator
}

func (d *DispatchingDeliveryActivator) Activate(ctx context.Context, build *db.Build, result BuildCallbackRequest) error {
	if d.Static != nil && hasStaticApps(result) {
		return d.Static.Activate(ctx, build, result)
	}
	return d.Container.Activate(ctx, build, result)
}

func (d *DispatchingDeliveryActivator) Rollback(ctx context.Context, req RollbackDeliveryRequest) error {
	return d.Container.Rollback(ctx, req)
}

// hasStaticApps checks if any app in the result has a static delivery type.
func hasStaticApps(result BuildCallbackRequest) bool {
	for _, app := range result.Apps {
		if app.Type == "frontend" {
			return true
		}
	}
	return false
}

// RollbackDeliveryRequest contains the delivery-specific inputs for rollback.
// State machine transitions are NOT part of this request — they are handled
// by the RollbackOrchestrator before and after calling DeliveryActivator.Rollback.
type RollbackDeliveryRequest struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string
	TargetRelease *db.Release // the release to reactivate
	CorrelationID string
}
