// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package enforcement

import (
	"errors"
	"fmt"
	"strings"
)

// J-20 Scale plan v1.1 T1.2 + T1.3 + EC-2/EC-3/EC-4 absorption.
//
// Server-side enforcement boundary for:
//   - KEDA scale-to-zero opt-in (plan tier ≥ Pro required) per ADR D3
//   - HPA target bounds (CPU ∈ [10,95], memory ∈ [10,90]) per EC-4
//   - Monthly cost computation (server-authoritative, NOT client) per ADR D1
//
// Client UI mirrors these for UX (disabled toggles, inline validation), but
// the API handler is the security boundary. Free user bypassing the dashboard
// via raw HTTP MUST be rejected here.

// HPA target bounds expressed as percent of pod resource utilization.
//
//   - Lower bound 10 prevents "useless HPA" (target=1% → trigger fires
//     before pod even warms up → permanent over-scaling).
//   - Upper bound 95 (CPU) / 90 (memory) leaves headroom for transient
//     spikes that would otherwise pin the pod at 100% and OOM.
//
// Matches the recommended canonical Kubernetes HPA practice + LoopRuntime plan v1.1
// EC-4 SHOULD TEST coverage.
const (
	HPACPUTargetMin    = 10
	HPACPUTargetMax    = 95
	HPAMemoryTargetMin = 10
	HPAMemoryTargetMax = 90
)

// ErrKEDARequiresPro is returned by AssertKEDAAllowed when a plan tier below
// Pro attempts to opt into KEDA scale-to-zero. Maps to HTTP 402 "Payment
// Required" at the handler boundary per ADR D3.
var ErrKEDARequiresPro = errors.New("KEDA scale-to-zero requires plan tier ≥ Pro")

// ErrHPATargetOutOfBounds is the sentinel that HPATargetBoundsError unwraps
// to — callers test via errors.Is(err, ErrHPATargetOutOfBounds) without
// caring about which field violated which bound.
var ErrHPATargetOutOfBounds = errors.New("HPA target out of bounds")

// HPATargetBoundsError carries which field violated which bound. Surfaces
// at the handler boundary as HTTP 400 with structured body:
//
//	{"error":"hpa_target_out_of_bounds","field":"cpu_target",
//	 "min":10,"max":95,"got":1}
type HPATargetBoundsError struct {
	Field string // "cpu_target" or "memory_target"
	Min   int
	Max   int
	Got   int
}

// Error implements error.
func (e *HPATargetBoundsError) Error() string {
	return fmt.Sprintf("hpa target out of bounds: field=%s min=%d max=%d got=%d",
		e.Field, e.Min, e.Max, e.Got)
}

// Unwrap returns the sentinel so errors.Is(err, ErrHPATargetOutOfBounds) holds.
func (e *HPATargetBoundsError) Unwrap() error { return ErrHPATargetOutOfBounds }

// kedaEligiblePlans is the canonical set of plan tiers permitted to enable
// KEDA scale-to-zero. Per CLAUDE.md pricing locked at MEET-2026-05-12 D4:
// Pro ($29/mo) is the minimum eligible tier; Team and Enterprise inherit.
//
// Unknown / empty plan tier defaults to REJECT (fail-closed) — a Free user
// bypassing the client UI via raw API call MUST land here, not in a
// permissive "allow if unrecognized" branch.
var kedaEligiblePlans = map[string]bool{
	string(PlanPro):        true,
	string(PlanTeam):       true,
	string(PlanEnterprise): true,
}

// AssertKEDAAllowed returns nil when planTier is Pro/Team/Enterprise and
// ErrKEDARequiresPro otherwise. Normalizes case + trims whitespace because
// the API handler receives the string from session context, which can be
// populated by sources of varying casing (CLI / Stripe webhook / seed).
//
// Fail-closed behavior on unknown/empty input is intentional per ADR D3.
func AssertKEDAAllowed(planTier string) error {
	normalized := strings.ToLower(strings.TrimSpace(planTier))
	if kedaEligiblePlans[normalized] {
		return nil
	}
	return ErrKEDARequiresPro
}

// AssertHPATargetBounds validates cpu_target ∈ [10,95] AND
// memory_target ∈ [10,90]. Returns *HPATargetBoundsError on the first
// violation found (CPU checked first — deterministic ordering for tests).
// Returns nil when both fields fall within bounds.
//
// Per plan v1.1 EC-4: server-side enforcement at handler boundary; client
// mirror is UX-only (immediate feedback). The handler MUST call this even
// when the client claims it already validated — never trust the client.
func AssertHPATargetBounds(cpuTarget, memoryTarget int) error {
	if cpuTarget < HPACPUTargetMin || cpuTarget > HPACPUTargetMax {
		return &HPATargetBoundsError{
			Field: "cpu_target",
			Min:   HPACPUTargetMin,
			Max:   HPACPUTargetMax,
			Got:   cpuTarget,
		}
	}
	if memoryTarget < HPAMemoryTargetMin || memoryTarget > HPAMemoryTargetMax {
		return &HPATargetBoundsError{
			Field: "memory_target",
			Min:   HPAMemoryTargetMin,
			Max:   HPAMemoryTargetMax,
			Got:   memoryTarget,
		}
	}
	return nil
}

// Scaling defaults. They are product rules — what a plan buys and what a
// `loopruntime scale` without explicit targets means — so they live here with the
// bounds and the cost formula, not in the HTTP handler that applies them.
const (
	// DefaultPodCostPerHour is the conservative per-pod hourly cost used when
	// no per-cluster rate has been wired. Matches the public pricing page at
	// https://getloopscale.com/pricing as of plan v1.1 sign-off. The dashboard's
	// cost preview must reconcile to what the server returns (ADR D1, EC-2).
	DefaultPodCostPerHour float64 = 0.05

	// DefaultHPAMaxReplicas is the autoscaling ceiling when none is given.
	DefaultHPAMaxReplicas = 3
	// DefaultHPACPUTarget and DefaultHPAMemoryTarget are the utilisation
	// targets (percent) when none is given — the Kubernetes HPA convention,
	// inside the HPA*Target bounds above.
	DefaultHPACPUTarget    = 80
	DefaultHPAMemoryTarget = 80
	// DefaultScaleToZeroCooldownSeconds is the idle period before a
	// min-0 workload scales to zero.
	DefaultScaleToZeroCooldownSeconds = 300
)

// ComputeMonthlyCost returns the authoritative $/month estimate for a given
// replica count + per-pod hourly cost. Server-side ONLY — client preview is
// non-authoritative per ADR D1 EC-2 absorption.
//
// Formula: replicas * podCostPerHour * 730 (avg hours per month, matches
// industry-standard cloud billing convention used by AWS / GCP / DO).
//
// Returns 0 when either input is non-positive (defensive — negative inputs
// can arrive when a downstream caller forgets to validate). Caller decides
// whether to surface "free tier" vs error in that case.
func ComputeMonthlyCost(replicas int, podCostPerHour float64) float64 {
	if replicas <= 0 || podCostPerHour <= 0 {
		return 0
	}
	const hoursPerMonth = 730.0
	return float64(replicas) * podCostPerHour * hoursPerMonth
}
