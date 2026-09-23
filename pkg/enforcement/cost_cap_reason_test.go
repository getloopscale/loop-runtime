// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The gate has to say WHY it allowed.
//
// B-033, measured on app-dev 2026-08-11: the aggregator that fills `cost_counters` is off
// (`LOOPZERO_PROMETHEUS_URL` missing from the Deployment), the table has **0 rows**, and the
// `Check` contract treats a missing row as zero usage — so **every** cost cap allows, and
// nothing tells that apart from "the tenant really used nothing".
//
// Returning the reason does not change the verdict: it allows the same. It changes what
// the system can observe about its own protection.
func TestCheckReason_DistinguishesUnmeasuredFromUnderCap(t *testing.T) {
	ctx := context.Background()

	noRow := newFakeCostStore()
	reason, err := NewCapChecker(noRow).CheckReason(ctx, "tn-x", PlanStarter, MetricBuildMinutes)
	if err != nil {
		t.Fatalf("a missing row must allow: %v", err)
	}
	if reason != AllowedUnmeasured {
		t.Errorf("missing row: reason %v, want AllowedUnmeasured", reason)
	}

	withRow := newFakeCostStore()
	withRow.put(CostCounter{TenantID: "tn-x", Metric: MetricBuildMinutes, Value: 1, PeriodStart: time.Now().Unix()})
	reason, err = NewCapChecker(withRow).CheckReason(ctx, "tn-x", PlanStarter, MetricBuildMinutes)
	if err != nil {
		t.Fatalf("under the cap must allow: %v", err)
	}
	if reason != AllowedUnderCap {
		t.Errorf("measured and under the cap: reason %v, want AllowedUnderCap", reason)
	}
}

// A store error also allows — and must also be distinguishable, because it is the second
// silent door: a noisy aggregator must not block production, but the operator has to know
// the protection evaluated nothing.
func TestCheckReason_StoreErrorIsItsOwnReason(t *testing.T) {
	broken := newFakeCostStore()
	broken.err = errors.New("pool exhausted")

	reason, err := NewCapChecker(broken).CheckReason(context.Background(), "tn-x", PlanStarter, MetricBuildMinutes)
	if err != nil {
		t.Fatalf("a store error allows (fail-open): %v", err)
	}
	if reason != AllowedStoreError {
		t.Errorf("store error: reason %v, want AllowedStoreError", reason)
	}
}

// Exceeding the cap still blocks, and the reason does not mask it.
func TestCheckReason_ExceededStillBlocks(t *testing.T) {
	full := newFakeCostStore()
	full.put(CostCounter{TenantID: "tn-x", Metric: MetricBuildMinutes, Value: 1 << 30, PeriodStart: time.Now().Unix()})

	reason, err := NewCapChecker(full).CheckReason(context.Background(), "tn-x", PlanStarter, MetricBuildMinutes)
	var ex *ExceededError
	if !errors.As(err, &ex) {
		t.Fatalf("over the cap must block, got %v (reason %v)", err, reason)
	}
	if reason != Blocked {
		t.Errorf("blocked: reason %v, want Blocked", reason)
	}
}

// `Check` keeps the same contract: a caller that only wants to know whether it may proceed
// does not need to learn the reason.
func TestCheck_KeepsItsContract(t *testing.T) {
	if err := NewCapChecker(newFakeCostStore()).Check(context.Background(), "tn-x", PlanStarter, MetricBuildMinutes); err != nil {
		t.Errorf("Check should allow on a missing row, got %v", err)
	}
}
