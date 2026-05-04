// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetReadinessSummary computes the kpt CRD Status Convention summary
// (Ready / Reconciling / Stalled) from per-step detail conditions.
//
// ADR-0025 D3 / plan T3.1: every LoopZero CRD exposes these three summary
// conditions so external tooling (Argo Rollouts, Flux, ArgoCD health,
// kubectl wait, Crossplane composition) consume LoopZero CRDs without
// bespoke status mappers.
//
// Algorithm (mutually exclusive, classification cascade):
//   - Stalled=True  iff any detail has status False with reason in
//                   {ReasonFailed, ReasonStalled, ReasonDegraded}.
//                   Stalled wins over Reconciling (failure takes priority).
//   - Reconciling=True iff (a) Stalled is not True AND
//                   (b) any detail is missing OR status Unknown OR
//                   status False with reason in
//                   {ReasonReconciling, ReasonProgressing, ReasonWaiting,
//                    ReasonNotFound, ReasonUnreachable, "Initializing"}.
//   - Ready=True    iff none of the above AND every detail has status
//                   True with ObservedGeneration == generation.
//
// Edge cases:
//   - Empty detailTypes → Ready=True with reason "NoDetailsRequired".
//   - Generation mismatch on any True detail → not Ready (controller
//     hasn't caught up to the latest spec yet); summary becomes
//     Reconciling=True.
//   - Idempotent: calling with identical inputs twice produces zero
//     LastTransitionTime change (SetCondition preserves transition time
//     when status is unchanged).
//
// MUST be the LAST status mutation in every Reconcile (per ADR-0025).
// Pair with `defer` so it runs even on early return / panic — see
// reference Reconcile pattern in ADR-0025.
func SetReadinessSummary(conds *[]metav1.Condition, generation int64, detailTypes []string) {
	if conds == nil {
		return
	}

	// Empty detail list — degenerate case, no work for controller. Mark
	// Ready=True so kstatus.Compute returns Current; consumers don't
	// hang waiting forever.
	if len(detailTypes) == 0 {
		SetCondition(conds, TypeReady, metav1.ConditionTrue, "NoDetailsRequired",
			"no detail conditions configured for this resource", generation)
		SetCondition(conds, TypeReconciling, metav1.ConditionFalse, "NoDetailsRequired", "", generation)
		SetCondition(conds, TypeStalled, metav1.ConditionFalse, "NoDetailsRequired", "", generation)
		return
	}

	var (
		stalledReasons    []string
		reconcilingMissing []string
		reconcilingDetail string
		allTrue           = true
		hasGenerationMismatch bool
	)

	for _, dt := range detailTypes {
		c := GetCondition(*conds, dt)
		if c == nil {
			allTrue = false
			reconcilingMissing = append(reconcilingMissing, dt)
			continue
		}
		switch c.Status {
		case metav1.ConditionTrue:
			// Generation match required for Ready (per kstatus.Compute semantics).
			if c.ObservedGeneration != 0 && c.ObservedGeneration != generation {
				allTrue = false
				hasGenerationMismatch = true
			}
		case metav1.ConditionFalse:
			allTrue = false
			switch c.Reason {
			case ReasonFailed, ReasonStalled, ReasonDegraded:
				stalledReasons = append(stalledReasons, dt+"="+c.Reason)
			default:
				// Reconciling/Progressing/Waiting/NotFound/Unreachable/etc.
				if reconcilingDetail == "" {
					reconcilingDetail = dt + "=" + c.Reason
				}
			}
		default:
			// Unknown: treat as in-progress (kstatus convention).
			allTrue = false
			if reconcilingDetail == "" {
				reconcilingDetail = dt + "=Unknown"
			}
		}
	}

	// Classification cascade — Stalled wins, then Reconciling, else Ready.
	switch {
	case len(stalledReasons) > 0:
		msg := joinDetails(stalledReasons)
		SetCondition(conds, TypeStalled, metav1.ConditionTrue, ReasonStalled, msg, generation)
		SetCondition(conds, TypeReconciling, metav1.ConditionFalse, ReasonStalled, "halted: "+msg, generation)
		SetCondition(conds, TypeReady, metav1.ConditionFalse, ReasonStalled, "halted: "+msg, generation)
	case !allTrue:
		// Reconciling — either still working or generation mismatch.
		var msg string
		switch {
		case len(reconcilingMissing) > 0:
			msg = "awaiting: " + joinDetails(reconcilingMissing)
		case hasGenerationMismatch:
			msg = "spec changed; controller catching up"
		default:
			msg = "in progress: " + reconcilingDetail
		}
		SetCondition(conds, TypeStalled, metav1.ConditionFalse, ReasonReconciling, "", generation)
		SetCondition(conds, TypeReconciling, metav1.ConditionTrue, ReasonReconciling, msg, generation)
		SetCondition(conds, TypeReady, metav1.ConditionFalse, ReasonReconciling, msg, generation)
	default:
		// All True with matching generation — fully ready.
		SetCondition(conds, TypeStalled, metav1.ConditionFalse, ReasonReady, "", generation)
		SetCondition(conds, TypeReconciling, metav1.ConditionFalse, ReasonReady, "", generation)
		SetCondition(conds, TypeReady, metav1.ConditionTrue, ReasonReady,
			"all detail conditions satisfied", generation)
	}
}

// joinDetails concatenates detail strings with a comma separator.
// Avoids importing strings just for Join in such a small helper.
func joinDetails(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
