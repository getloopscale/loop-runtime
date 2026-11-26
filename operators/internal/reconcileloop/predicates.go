// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package reconcileloop

import (
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// OperatorPredicates returns the canonical predicate stack applied to all
// 8 LoopRuntime controllers' For() watches.
//
// Per ADR-0023 / plan T2.1 + roadmap §QW-1: zero predicates today produces
// 50–500x reconcile amplification because every Status().Update fires
// the controller's own watch. The default ResourceVersionChangedPredicate
// fires on EVERY change including status mutations.
//
// Composition (logical OR — any True triggers reconcile):
//
//   - GenerationChangedPredicate: spec changes
//   - AnnotationChangedPredicate: runtime.getloopscale.com/heal-trigger, ArgoCD annotations,
//     cell pin overrides, etc.
//   - LabelChangedPredicate: app.kubernetes.io/managed-by, runtime.getloopscale.com/cell-id
//
// Status updates do NOT bump generation/annotations/labels; therefore the
// controller's own Status().Update calls are filtered.
//
// Edge cases:
//   - Initial Create: GenerationChangedPredicate.Create returns true
//     (controller-runtime source). New objects always reconcile.
//   - Finalizer add: metadata-only, but controller-runtime's watch event
//     fires regardless because resource version changes. Predicate filters
//     event TYPES not API operations.
//   - Spec change (tenant.Spec.Plan etc.): generation bumps → reconcile.
//   - Annotation update (loop-runtime admin tenants pin → runtime.getloopscale.com/cell-id):
//     reconcile via AnnotationChangedPredicate.
//
// Apply at controller setup:
//
//	ctrl.NewControllerManagedBy(mgr).
//	    For(&loopruntimev1.Tenant{}, builder.WithPredicates(reconcileloop.OperatorPredicates())).
//	    Owns(...).
//	    Complete(...)
//
// For Watches() of resources without generation (Secret, ConfigMap), do
// NOT apply this predicate — Secret content has no generation. Use the
// default ResourceVersionChangedPredicate (no extra opts).
func OperatorPredicates() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
		predicate.LabelChangedPredicate{},
	)
}

// ForOption is a convenience that wraps OperatorPredicates() in
// builder.WithPredicates() so call sites stay one-line:
//
//	For(&loopruntimev1.Tenant{}, reconcileloop.ForOption())
//
// Equivalent to:
//
//	For(&loopruntimev1.Tenant{}, builder.WithPredicates(reconcileloop.OperatorPredicates()))
func ForOption() builder.ForOption {
	return builder.WithPredicates(OperatorPredicates())
}
