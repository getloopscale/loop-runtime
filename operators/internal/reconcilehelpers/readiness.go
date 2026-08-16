// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package reconcilehelpers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/getloopscale/loopzero/operators/internal/conditions"
)

// DeferReadinessSummary returns a deferred function suitable for `defer` at
// the top of any reconcile loop. It re-fetches the CRD by key, skips if
// deleted, then writes the kstatus universal Ready summary (ADR-0025 D3)
// and persists it with a merge patch.
//
// Phase 3 cross-package helper: previously this ~10-line defer block was
// inlined verbatim in the 5 PROD reconcile loops below because ADR-0025 mandates the
// readiness summary be the LAST status mutation per CRD, even on early
// return — so every controller had a copy:
//
//   - internal/platform/clustercredential_controller.go
//   - internal/platform/gitrepositorybinding_controller.go
//   - internal/platform/platformbootstrap_controller.go
//   - internal/preview/preview_controller.go
//   - internal/environment/environment_controller.go
//
// Usage:
//
//	defer reconcilehelpers.DeferReadinessSummary(ctx, r.Client, req.NamespacedName,
//	    func() *loopzerov1.X { return &loopzerov1.X{} },
//	    func(x *loopzerov1.X) *[]metav1.Condition { return &x.Status.Conditions },
//	    xDetailTypes)()
//
// Why re-fetch instead of using the local pointer: helpers invoked between
// the local Get and the defer firing (resolveKubeconfig, checkConnectivity,
// reconcileDelete, etc.) perform their own status writes. Using the local
// pointer would overwrite their work with a stale snapshot.
//
// Why an optimistically-locked merge patch and not Status().Update
// (ADR 2026-08-12-status-writer-contract):
//
//   - Status().Update replays the WHOLE status subresource as read at the Get
//     above, so every status field the controller wrote — phase,
//     observedGeneration, anything — is re-sent at its snapshot value. A merge
//     patch sends only `status.conditions`, the one subtree this helper owns.
//
//   - The lock is NOT optional here. A plain client.MergeFrom would drop the
//     resourceVersion, and RFC 7386 treats an array as a scalar: the conditions
//     patch carries the ENTIRE array, including conditions authored by the
//     controller. Without the lock, a write landing between this Get and this
//     patch would be silently erased. With it, that same race surfaces as a 409
//     and the next reconcile recomputes the summary — a loud retry instead of a
//     quiet loss.
//
// So the pair matters: the patch narrows WHAT is written, the lock keeps the
// conflict detection that Update got for free. This is the CLAUDE.md
// single-status-writer invariant applied to the one place that is, by
// construction, a second writer.
//
// The error is logged rather than returned: a `defer` has nowhere to return to,
// and a dropped status write must not be silent. NotFound is expected — the
// object may be deleted between the Get and the patch — so it is not an error.
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
		base := latest.DeepCopyObject().(client.Object)
		conditions.SetReadinessSummaryViaPkg(extractConditions(latest), latest.GetGeneration(), detailTypes)
		err := c.Status().Patch(ctx, latest, client.MergeFromWithOptions(base,
			client.MergeFromWithOptimisticLock{}))
		if err != nil && !apierrors.IsNotFound(err) {
			logf.FromContext(ctx).Error(err, "failed to persist readiness summary",
				"name", key.Name, "namespace", key.Namespace)
		}
	}
}
