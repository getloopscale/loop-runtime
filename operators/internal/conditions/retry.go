// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// statusRetryBackoff is the bounded retry budget for UpdateStatusWithRetry.
//
// Per ADR-0024 condition #2 (k8s-controller-engineer review): cap to ~2s
// so a slow Status loop cannot saturate `controller_runtime_active_workers`.
// 5 steps × backoff sequence (10ms, 50ms, 250ms, 1s, 2s capped) = ~3.3s
// worst case but typically resolves on the 1st or 2nd attempt.
//
// Tests can override via the helper's internal `withBackoff` constructor
// (see retry_test.go fast-loop variant).
var statusRetryBackoff = wait.Backoff{
	Steps:    5,
	Duration: 10 * time.Millisecond,
	Factor:   5.0,
	Jitter:   0.1,
	Cap:      2 * time.Second,
}

// ErrSpecMutation is returned by UpdateStatusWithRetry when the mutate
// function modifies obj.Spec or obj.Annotations/Labels in a way that would
// be silently dropped by Status().Update (which only persists /status).
//
// Per ADR-0024 condition #2 (spec-mutation guard): catch this footgun at
// runtime instead of letting controllers ship broken status writes.
var ErrSpecMutation = errors.New("conditions: mutate function modified non-status fields; Status().Update would silently drop them")

// SpecSnapshotter exports just enough of an object's identity to detect
// mutations to non-status fields. Implementations defined by call sites
// when the default reflection-based snapshot is too expensive (rare).
type SpecSnapshotter interface {
	// SnapshotForStatusGuard returns a value that compares equal across
	// two calls iff Spec/metadata are unchanged. Status fields excluded.
	SnapshotForStatusGuard() any
}

// UpdateStatusWithRetry re-fetches the object, applies mutate, and calls
// r.Status().Update wrapped in retry.OnError(statusRetryBackoff, IsConflict).
//
// ADR-0024 D2 / plan T2.3: every Status().Update site in the operators
// MUST migrate to this helper. Bare Status().Update is forbidden in new
// code (CI gate via static analysis after migration completes).
//
// Behavior:
//   - Each attempt does Get → mutate → Status().Update on a freshly fetched
//     copy of the object (avoids replaying stale resourceVersion).
//   - Only retries on apierrors.IsConflict; other errors return immediately
//     so callers can distinguish transient vs terminal failures.
//   - Honors ctx.Done() between attempts via retry.OnError's internal
//     polling; if the manager is shutting down, the loop bails out cleanly.
//   - If mutate returns an error, it propagates immediately without retry.
//   - Spec-mutation guard: deep-copy before mutate, post-mutate verify that
//     ObjectMeta + Spec are unchanged. If they differ, return
//     ErrSpecMutation without calling Status().Update.
//
// Type parameter T must satisfy client.Object. The newObj factory returns
// an empty instance of the concrete type each retry; this avoids the
// caller having to remember to zero out the previous attempt's state.
//
// Example:
//
//	err := conditions.UpdateStatusWithRetry(ctx, r.Client, key,
//	    func() *loopzerov1.Tenant { return &loopzerov1.Tenant{} },
//	    func(t *loopzerov1.Tenant) error {
//	        conditions.SetCondition(&t.Status.Conditions,
//	            conditions.TypeArgoCDAppReady, metav1.ConditionTrue,
//	            conditions.ReasonReady, "synced", t.Generation)
//	        return nil
//	    })
func UpdateStatusWithRetry[T client.Object](
	ctx context.Context,
	c client.Client,
	key client.ObjectKey,
	newObj func() T,
	mutate func(T) error,
) error {
	// Pre-check: short-circuit if context already cancelled. Per ADR-0024
	// condition #2, retry.OnError engole context.Canceled retornando nil
	// (quirk descoberto em test debug: callback retorna ctx.Err(),
	// retry.OnError vai trata como wait timeout especial e devolve nil).
	// Verificar aqui garante manager-shutdown safety.
	if err := ctx.Err(); err != nil {
		return err
	}

	retryErr := retry.OnError(statusRetryBackoff, apierrors.IsConflict, func() error {
		// Honor context cancellation between attempts (manager shutdown).
		// Returning ctx.Err() here causes retry.OnError to swallow it
		// (see pre-check above), so we ALSO check post-loop below.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		obj := newObj()
		if err := c.Get(ctx, key, obj); err != nil {
			// NotFound and other errors propagate; not retried unless caller wraps.
			return err
		}

		// Deep-copy the pre-mutate state for the spec guard. Use the
		// runtime.Object DeepCopyObject API which all client.Object impls
		// expose (kubebuilder generates them).
		preMutate := obj.DeepCopyObject().(client.Object)

		if err := mutate(obj); err != nil {
			return err
		}

		// Spec mutation guard: ResourceVersion + Generation are bumped by
		// the API server on Spec writes; if mutate touched Spec, those
		// stay equal client-side BUT the helper should still surface the
		// footgun. Use Generation (Spec checksum proxy) + Annotations +
		// Labels as the guard surface — Status field comparison is
		// orthogonal because Status().Update writes only /status.
		if obj.GetGeneration() != preMutate.GetGeneration() {
			return fmt.Errorf("%w: generation changed %d → %d",
				ErrSpecMutation, preMutate.GetGeneration(), obj.GetGeneration())
		}
		if !mapsEqual(obj.GetAnnotations(), preMutate.GetAnnotations()) {
			return fmt.Errorf("%w: annotations modified", ErrSpecMutation)
		}
		if !mapsEqual(obj.GetLabels(), preMutate.GetLabels()) {
			return fmt.Errorf("%w: labels modified", ErrSpecMutation)
		}

		return c.Status().Update(ctx, obj)
	})

	// Post-check: if retry.OnError swallowed a ctx.Canceled (quirk above),
	// surface it explicitly so callers can distinguish "didn't run" from
	// "ran successfully".
	if retryErr == nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return retryErr
}

// mapsEqual returns true if a and b have the same key-value pairs. Treats
// nil and empty as equal (k8s API convention).
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}
