// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shared

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loopzero/operators/internal/conditions"
)

// DeferReadinessSummary returns a deferred function suitable for `defer` at
// the top of any reconcile loop. It re-fetches the CRD by key, skips if
// deleted, then writes the kstatus universal Ready summary (ADR-0025 D3)
// and Status-updates.
//
// Phase 3 cross-package helper: previously this ~10-line defer block was
// inlined verbatim in 5 PROD reconcile loops because ADR-0025 mandates the
// readiness summary be the LAST status mutation per CRD, even on early
// return — so every controller had a copy:
//
//   - internal/platform/clustercredential_controller.go
//   - internal/platform/gitrepositorybinding_controller.go
//   - internal/platform/platformbootstrap_controller.go
//   - internal/preview/preview_controller.go
//   - internal/tenant/environment_controller.go
//
// Usage:
//
//	defer shared.DeferReadinessSummary(ctx, r.Client, req.NamespacedName,
//	    func() *loopzerov1.X { return &loopzerov1.X{} },
//	    func(x *loopzerov1.X) *[]metav1.Condition { return &x.Status.Conditions },
//	    xDetailTypes)()
//
// Why re-fetch instead of using the local pointer: helpers invoked between
// the local Get and the defer firing (resolveKubeconfig, checkConnectivity,
// reconcileDelete, etc.) perform their own Status().Update calls. Using
// the local pointer would overwrite their work with a stale snapshot.
func DeferReadinessSummary[T client.Object](
	ctx context.Context,
	c client.Client,
	key client.ObjectKey,
	factory func() T,
	extractConditions func(T) *[]metav1.Condition,
	detailTypes []string,
) func() {
	return func() {
		latest := factory()
		if err := c.Get(ctx, key, latest); err != nil {
			return
		}
		if !latest.GetDeletionTimestamp().IsZero() {
			return
		}
		conditions.SetReadinessSummaryViaPkg(extractConditions(latest), latest.GetGeneration(), detailTypes)
		_ = c.Status().Update(ctx, latest)
	}
}
