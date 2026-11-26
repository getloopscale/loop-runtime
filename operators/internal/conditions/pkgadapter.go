// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// pkgadapter.go bridges this operator-internal `conditions` package to
// the cross-cutting `pkg/conditions` shared layer introduced by
// ADR-0032. The bridge is intentionally thin — operators already
// committed to `metav1.Condition` per ADR-0025 D3, and pkg/conditions
// uses its own portable struct (no apimachinery dependency in pkg/),
// so we convert at the boundary instead of forcing one side to switch.
//
// Pattern (per pkg-sota-faang-grade-plan v1.2 Adoption Follow-up #1):
//
//   var summary []metav1.Condition = ...   // controller's slice
//   conditions.SetReadinessSummaryViaPkg(&summary, generation, detailTypes)
//   // ↑ delegates to pkg/conditions.SetReadinessSummary internally
//
// This proves the pkg/conditions algorithmic core is the single source
// of truth; controllers keep the metav1.Condition shape that kubectl,
// kstatus.Compute, and ArgoCD's health controllers expect.

package conditions

import (
	pkgconditions "github.com/getloopscale/loop-runtime/pkg/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fromMetav1 converts a metav1.Condition slice to the portable
// pkg/conditions.Condition representation.
func fromMetav1(conds []metav1.Condition) []pkgconditions.Condition {
	out := make([]pkgconditions.Condition, len(conds))
	for i, c := range conds {
		out[i] = pkgconditions.Condition{
			Type:               c.Type,
			Status:             pkgconditions.Status(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime.Time,
			ObservedGeneration: c.ObservedGeneration,
		}
	}
	return out
}

// toMetav1 converts the portable pkg/conditions.Condition slice back to
// metav1.Condition. Used after pkg/conditions mutates the slice so the
// controller status field stays metav1-typed.
func toMetav1(conds []pkgconditions.Condition) []metav1.Condition {
	out := make([]metav1.Condition, len(conds))
	for i, c := range conds {
		out[i] = metav1.Condition{
			Type:               c.Type,
			Status:             metav1.ConditionStatus(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: metav1.NewTime(c.LastTransitionTime),
			ObservedGeneration: c.ObservedGeneration,
		}
	}
	return out
}

// SetReadinessSummaryViaPkg delegates to pkg/conditions.SetReadinessSummary
// after a transparent metav1 <-> pkg/conditions conversion. It is the only
// readiness-summary entry point in the operators: the algorithm lives once,
// in pkg/conditions (ADR-0032), and this adapter only changes the shape.
func SetReadinessSummaryViaPkg(conds *[]metav1.Condition, generation int64, detailTypes []string) {
	if conds == nil {
		return
	}
	portable := fromMetav1(*conds)
	pkgconditions.SetReadinessSummary(&portable, generation, detailTypes)
	*conds = toMetav1(portable)
}
