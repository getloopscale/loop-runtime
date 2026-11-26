// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package operatortest

import (
	"context"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateWithRetry re-reads obj, applies mutate, and writes it back — retrying
// on the 409 Conflict a concurrently-reconciling controller produces.
//
// In an envtest suite the manager is running, so the controller writes to the
// very objects the specs mutate (finalizers, status, labels). A bare
//
//	Get(ctx, key, &obj); obj.Spec.X = y; Update(ctx, &obj)
//
// therefore races the reconcile loop and loses on resourceVersion:
//
//	Operation cannot be fulfilled on tenants.runtime.getloopscale.com "…": the object has been
//	modified; please apply your changes to the latest version and try again
//
// Measured 2026-08-06: 3 of 4 consecutive runs of internal/tenant failed this
// way, each on a DIFFERENT spec — the signature of a race, not of a broken
// assertion. Retrying is what the production code already does for the same
// hazard (internal/argocd/real_provisioner.go:301).
//
// mutate MUST apply its changes to obj itself, because obj is re-fetched (and
// so overwritten) before every attempt. Assigning fields to a captured copy
// taken before the call silently loses them on a retry.
func UpdateWithRetry(ctx context.Context, c client.Client, obj client.Object, mutate func()) error {
	key := client.ObjectKeyFromObject(obj)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, key, obj); err != nil {
			return err
		}
		mutate()
		return c.Update(ctx, obj)
	})
}
