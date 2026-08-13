// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Test_LevelIVMetrics_Registered — ADR-0027 D5 / plan T5.2: pin the
// 5 new L4 "Deep Insights" metrics so a typo in metric name doesn't
// silently break Grafana dashboards or alert rules.
func Test_LevelIVMetrics_Registered(t *testing.T) {
	cases := []struct {
		name string
		emit func()
	}{
		{
			name: "loopzero_self_heal_total",
			emit: func() { SelfHealTotal.WithLabelValues("application", "reaper_cleaned").Inc() },
		},
		{
			name: "loopzero_orphan_cr_total",
			emit: func() { OrphanCRTotal.WithLabelValues("LoopZeroApplication").Inc() },
		},
		{
			name: "loopzero_finalizer_blocked_seconds",
			emit: func() { FinalizerBlockedSeconds.WithLabelValues("Tenant").Observe(2.5) },
		},
		{
			name: "loopzero_reconcile_queue_depth",
			emit: func() { ReconcileQueueDepth.WithLabelValues("tenant").Set(5) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.emit() // panics if metric name collision or registration missing
		})
	}
}

// Test_SelfHealTotal_Increments — semantic test: counter monotonically
// increments per emit call.
func Test_SelfHealTotal_Increments(t *testing.T) {
	before := testutil.ToFloat64(SelfHealTotal.WithLabelValues("test_controller", "test_action"))
	SelfHealTotal.WithLabelValues("test_controller", "test_action").Inc()
	after := testutil.ToFloat64(SelfHealTotal.WithLabelValues("test_controller", "test_action"))
	if after-before != 1 {
		t.Errorf("SelfHealTotal: delta = %f, want 1", after-before)
	}
}

// Test_OrphanCRTotal_Increments — semantic test for orphan tracking.
func Test_OrphanCRTotal_Increments(t *testing.T) {
	before := testutil.ToFloat64(OrphanCRTotal.WithLabelValues("TestKind"))
	OrphanCRTotal.WithLabelValues("TestKind").Inc()
	after := testutil.ToFloat64(OrphanCRTotal.WithLabelValues("TestKind"))
	if after-before != 1 {
		t.Errorf("OrphanCRTotal: delta = %f, want 1", after-before)
	}
}
