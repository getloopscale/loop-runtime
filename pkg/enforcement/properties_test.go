// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T4.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md —
// property-based extension for PlanLimiter invariants. Closes M04 LOW
// gap (property-based testing escopo limitado).
//
// These properties target the contract surface that prod relies on:
//   1. Enterprise plan NEVER denies — regardless of currentCount
//      (CheckLimit) or requested (CheckValue).
//   2. Hard limit < 0 means unlimited — currentCount cannot trigger deny.
//   3. CheckLimit is deterministic — same inputs → same LimitResult.
//   4. Negative currentCount is a programming error, not a denial signal
//      (logger MAY warn, but result must follow Plan + Metric).

package enforcement

import (
	"context"
	"testing"
	"testing/quick"
)

// TestProperty_EnterprisePlanAlwaysAllowsCheckLimit — for any tenant
// pinned to "enterprise", any metric, any currentCount, CheckLimit
// MUST return Allowed=true. Plan = "enterprise".
func TestProperty_EnterprisePlanAlwaysAllowsCheckLimit(t *testing.T) {
	f := func(metric string, currentCount uint32) bool {
		store := &fakeLimitsStore{
			plans:  map[string]string{"tn-enterprise": "enterprise"},
			limits: map[[2]string][2]int{}, // none — enterprise short-circuits
		}
		limiter := NewPlanLimiter(store, testLogger())
		got, err := limiter.CheckLimit(context.Background(), "tn-enterprise", metric, int(currentCount))
		requireNoLimitErr(t, err)
		return got.Allowed && got.Plan == "enterprise"
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("enterprise plan must always allow CheckLimit: %v", err)
	}
}

// TestProperty_EnterprisePlanAlwaysAllowsCheckValue — same invariant
// for CheckValue path (replicas, build_timeout, etc.).
func TestProperty_EnterprisePlanAlwaysAllowsCheckValue(t *testing.T) {
	f := func(metric string, requested uint32) bool {
		store := &fakeLimitsStore{
			plans:  map[string]string{"tn-enterprise": "enterprise"},
			limits: map[[2]string][2]int{},
		}
		limiter := NewPlanLimiter(store, testLogger())
		got, err := limiter.CheckValue(context.Background(), "tn-enterprise", metric, int(requested))
		requireNoLimitErr(t, err)
		return got.Allowed && got.Plan == "enterprise"
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("enterprise plan must always allow CheckValue: %v", err)
	}
}

// TestProperty_UnlimitedHardLimitAlwaysAllows — when hardLimit == -1
// (unlimited per ADR), ANY currentCount must Allow.
func TestProperty_UnlimitedHardLimitAlwaysAllows(t *testing.T) {
	f := func(currentCount uint32) bool {
		store := &fakeLimitsStore{
			plans:  map[string]string{"tn-team": "team"},
			limits: map[[2]string][2]int{{"team", "projects"}: {-1, 0}},
		}
		limiter := NewPlanLimiter(store, testLogger())
		got, err := limiter.CheckLimit(context.Background(), "tn-team", "projects", int(currentCount))
		requireNoLimitErr(t, err)
		return got.Allowed
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("hardLimit==-1 must always allow: %v", err)
	}
}

// TestProperty_CheckLimitDeterministic — same inputs must yield
// identical LimitResult across N invocations. Catches regressions
// where the limiter is accidentally made stateful.
func TestProperty_CheckLimitDeterministic(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "starter"},
		limits: map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())

	f := func(count uint8) bool {
		first, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", int(count))
		requireNoLimitErr(t, err)
		for i := 0; i < 20; i++ {
			r, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", int(count))
			requireNoLimitErr(t, err)
			if r.Allowed != first.Allowed || r.Plan != first.Plan ||
				r.Limit != first.Limit || r.Current != first.Current {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("CheckLimit must be deterministic: %v", err)
	}
}

// TestProperty_CheckLimit_HardLimitBoundary — at exactly hardLimit,
// must deny. At hardLimit-1, must allow (for non-zero hard limit).
func TestProperty_CheckLimit_HardLimitBoundary(t *testing.T) {
	f := func(hard uint8) bool {
		if hard < 2 || hard > 100 {
			return true // out of meaningful range
		}
		store := &fakeLimitsStore{
			plans:  map[string]string{"tn-acme": "starter"},
			limits: map[[2]string][2]int{{"starter", "projects"}: {int(hard), 0}},
		}
		limiter := NewPlanLimiter(store, testLogger())

		under, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", int(hard)-1)
		requireNoLimitErr(t, err)
		at, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", int(hard))
		requireNoLimitErr(t, err)
		over, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", int(hard)+1)
		requireNoLimitErr(t, err)

		return under.Allowed && !at.Allowed && !over.Allowed
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatalf("hard limit boundary semantics violated: %v", err)
	}
}

// TestProperty_CheckValue_RequestExceedsCap — CheckValue must deny
// when requested > hardLimit (CheckValue treats requested as the
// candidate value, not currentCount).
func TestProperty_CheckValue_RequestExceedsCap(t *testing.T) {
	f := func(hard uint8) bool {
		if hard < 1 || hard > 100 {
			return true
		}
		store := &fakeLimitsStore{
			plans:  map[string]string{"tn-acme": "starter"},
			limits: map[[2]string][2]int{{"starter", "replicas"}: {int(hard), 0}},
		}
		limiter := NewPlanLimiter(store, testLogger())

		under, err := limiter.CheckValue(context.Background(), "tn-acme", "replicas", int(hard))
		requireNoLimitErr(t, err)
		over, err := limiter.CheckValue(context.Background(), "tn-acme", "replicas", int(hard)+1)
		requireNoLimitErr(t, err)

		// At exactly hard: CheckValue should ALLOW (== cap is fine).
		// Above hard: must DENY.
		return under.Allowed && !over.Allowed
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatalf("CheckValue boundary: %v", err)
	}
}
