// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"testing"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/metering"
)

func TestTrackerNilDB_RecordBuild(t *testing.T) {
	tracker := metering.NewTracker(nil, nil)
	if err := tracker.RecordBuildCompletion("tn-acme", 300); err != nil {
		t.Errorf("RecordBuildCompletion with nil DB should not error: %v", err)
	}
}

func TestTrackerNilDB_RecordStorage(t *testing.T) {
	tracker := metering.NewTracker(nil, nil)
	if err := tracker.RecordStorageSnapshot("tn-acme", 1024*1024*500); err != nil {
		t.Errorf("RecordStorageSnapshot with nil DB should not error: %v", err)
	}
}

func TestTrackerNilDB_GetUsageSummary(t *testing.T) {
	tracker := metering.NewTracker(nil, nil)
	summary, err := tracker.GetUsageSummary("tn-acme")
	if err != nil {
		t.Errorf("GetUsageSummary with nil DB should not error: %v", err)
	}
	if len(summary) != 0 {
		t.Errorf("summary should be empty with nil DB, got %v", summary)
	}
}

func TestReporterNilDB(t *testing.T) {
	reporter := metering.NewReporter(nil, nil, nil)
	if err := reporter.ReportPending(); err != nil {
		t.Errorf("ReportPending with nil DB should not error: %v", err)
	}
}

func TestReconcilerNilDB(t *testing.T) {
	reconciler := metering.NewReconciler(nil, nil)
	if err := reconciler.ReconcileBuilds(); err != nil {
		t.Errorf("ReconcileBuilds with nil DB should not error: %v", err)
	}
}

func TestReconcilerDriftCallback(t *testing.T) {
	reconciler := metering.NewReconciler(nil, nil)

	var called bool
	reconciler.DriftCallback = func(tenantID, metric string, recorded, actual int, driftPct float64) {
		called = true
	}

	// With nil DB, reconciler skips — callback not called
	reconciler.ReconcileBuilds()
	if called {
		t.Error("DriftCallback should not be called with nil DB")
	}
}

func TestDriftThreshold(t *testing.T) {
	if metering.DriftThresholdPct != 5.0 {
		t.Errorf("DriftThresholdPct = %f, want 5.0", metering.DriftThresholdPct)
	}
}

func TestSchedulerRespectsContextCancellation(t *testing.T) {
	reporter := metering.NewReporter(nil, nil, nil)
	reconciler := metering.NewReconciler(nil, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		metering.StartScheduler(ctx, reporter, reconciler, nil)
		close(done)
	}()

	// Cancel immediately — scheduler should stop within 1 second
	cancel()

	select {
	case <-done:
		// OK — scheduler stopped
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2 seconds after context cancellation")
	}
}
