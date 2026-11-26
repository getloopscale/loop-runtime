// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package reconcileloop

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EnsureFinalizer adds the finalizer to obj if absent, persists via Update,
// and re-fetches obj (in place) to avoid stale resourceVersion errors on
// subsequent Status().Update calls inside the same reconcile.
//
// Returns nil if the finalizer was already present (no API calls).
// Returns the Update or Get error otherwise.
//
// Phase 3 cross-package helper: previously this ~8-line block was inlined
// verbatim in 6 PROD reconcile loops because every CRD with cleanup needs
// the same add-finalizer-then-re-fetch dance to keep the local pointer
// valid for downstream Status().Update calls:
//
//   - operators/internal/app/application_controller.go
//   - operators/internal/app/exposure_controller.go
//   - operators/internal/platform/clustercredential_controller.go
//   - operators/internal/platform/gitrepositorybinding_controller.go
//   - operators/internal/tenant/environment_controller.go
//   - operators/internal/tenant/tenant_controller.go
//
// Usage:
//
//	if err := reconcileloop.EnsureFinalizer(ctx, r.Client, &x, xFinalizer, req.NamespacedName); err != nil {
//	    return ctrl.Result{}, err
//	}
//
// The re-fetch is non-negotiable: Update bumps resourceVersion, and any
// subsequent Status().Update on the local pointer would fail with
// "the object has been modified" unless the local snapshot is refreshed.
func EnsureFinalizer(ctx context.Context, c client.Client, obj client.Object, finalizer string, key client.ObjectKey) error {
	if controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}
	controllerutil.AddFinalizer(obj, finalizer)
	if err := c.Update(ctx, obj); err != nil {
		return err
	}
	return c.Get(ctx, key, obj)
}
