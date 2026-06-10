// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import "strings"

// SetReadinessSummary computes the kstatus universal summary
// (Ready / Reconciling / Stalled) from per-step detail conditions
// and writes the three summary conditions on the slice.
//
// Algorithm (mutually exclusive cascade):
//
//   - Stalled=True iff any detail has Status==False AND its Reason is
//     in {ReasonFailed, ReasonStalled, ReasonDegraded}. Stalled wins
//     over Reconciling.
//   - Reconciling=True iff (a) Stalled is not True AND (b) any detail
//     is missing OR Status==Unknown OR Status==False with a non-stalled
//     Reason.
//   - Ready=True iff every detail is Status==True with
//     ObservedGeneration matching the resource generation.
//
// Edge cases:
//
//   - Empty detailTypes → Ready=True with reason "NoDetailsRequired"
//     so kstatus consumers do not hang waiting forever.
//   - ObservedGeneration mismatch on a True detail → Reconciling=True
//     with a "spec changed" message.
//   - Unknown status → treated as in-progress.
//   - Idempotent: repeated calls with the same inputs do NOT advance
//     LastTransitionTime (SetCondition preserves it when Status holds).
//
// Per ADR-0032 (mirrors operators/internal/conditions.SetReadinessSummary
// per ADR-0025; algorithmic core is identical).
//
// MUST be the LAST status mutation in every Reconcile so the summary
// reflects the latest detail snapshot. Pair with `defer` so it runs
// even on early return / panic.
func SetReadinessSummary(conds *[]Condition, generation int64, detailTypes []string) {
	if conds == nil {
		return
	}

	if len(detailTypes) == 0 {
		SetCondition(conds, TypeReady, StatusTrue, "NoDetailsRequired",
			"no detail conditions configured for this resource", generation)
		SetCondition(conds, TypeReconciling, StatusFalse, "NoDetailsRequired", "", generation)
		SetCondition(conds, TypeStalled, StatusFalse, "NoDetailsRequired", "", generation)
		return
	}

	var (
		stalledHits           []string
		missing               []string
		reconcilingDetail     string
		allTrue               = true
		hasGenerationMismatch bool
	)

	for _, dt := range detailTypes {
		c := GetCondition(*conds, dt)
		if c == nil {
			allTrue = false
			missing = append(missing, dt)
			continue
		}
		switch c.Status {
		case StatusTrue:
			if c.ObservedGeneration != 0 && c.ObservedGeneration != generation {
				allTrue = false
				hasGenerationMismatch = true
			}
		case StatusFalse:
			allTrue = false
			if _, isStalled := stalledReasons[c.Reason]; isStalled {
				stalledHits = append(stalledHits, dt+"="+c.Reason)
			} else if reconcilingDetail == "" {
				reconcilingDetail = dt + "=" + c.Reason
			}
		default: // StatusUnknown or any non-canonical value
			allTrue = false
			if reconcilingDetail == "" {
				reconcilingDetail = dt + "=Unknown"
			}
		}
	}

	switch {
	case len(stalledHits) > 0:
		msg := strings.Join(stalledHits, ", ")
		SetCondition(conds, TypeStalled, StatusTrue, ReasonStalled, msg, generation)
		SetCondition(conds, TypeReconciling, StatusFalse, ReasonStalled, "halted: "+msg, generation)
		SetCondition(conds, TypeReady, StatusFalse, ReasonStalled, "halted: "+msg, generation)
	case !allTrue:
		var msg string
		switch {
		case len(missing) > 0:
			msg = "awaiting: " + strings.Join(missing, ", ")
		case hasGenerationMismatch:
			msg = "spec changed; controller catching up"
		default:
			msg = "in progress: " + reconcilingDetail
		}
		SetCondition(conds, TypeStalled, StatusFalse, ReasonReconciling, "", generation)
		SetCondition(conds, TypeReconciling, StatusTrue, ReasonReconciling, msg, generation)
		SetCondition(conds, TypeReady, StatusFalse, ReasonReconciling, msg, generation)
	default:
		SetCondition(conds, TypeStalled, StatusFalse, ReasonReady, "", generation)
		SetCondition(conds, TypeReconciling, StatusFalse, ReasonReady, "", generation)
		SetCondition(conds, TypeReady, StatusTrue, ReasonReady,
			"all detail conditions satisfied", generation)
	}
}
