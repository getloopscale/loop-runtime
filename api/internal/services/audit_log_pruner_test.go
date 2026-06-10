// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"testing"
	"time"
)

// TestNextRunAt_TodayBeforeHour_ReturnsToday — pruner scheduling unit test.
//
// T7.3 of deep-review-remediation-plan. Validates that the next-run-time
// helper correctly handles the "early morning" boundary case: if the
// pruner starts at 14:00 local and the schedule is 02:00, it should
// schedule for 02:00 the NEXT day, not 02:00 today (already past).
//
// The helper is independent of pool/database — pure time arithmetic —
// so this test runs in isolation without LIVE infra.
func TestNextRunAt_TodayBeforeHour_ReturnsToday(t *testing.T) {
	// Now = 2026-05-09 01:30 local. Schedule = 02:00. Next run is 02:00 today.
	now := time.Date(2026, 5, 9, 1, 30, 0, 0, time.Local)
	got := nextRunAt(now, 2)
	want := time.Date(2026, 5, 9, 2, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextRunAt_TodayAfterHour_ReturnsTomorrow(t *testing.T) {
	// Now = 2026-05-09 14:00 local. Schedule = 02:00. Next run is 02:00 tomorrow.
	now := time.Date(2026, 5, 9, 14, 0, 0, 0, time.Local)
	got := nextRunAt(now, 2)
	want := time.Date(2026, 5, 10, 2, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextRunAt_ExactlyAtHour_ReturnsTomorrow(t *testing.T) {
	// Edge case: now == scheduled hour exactly. !After means strictly greater,
	// so candidate (== now) does NOT satisfy and rolls to next day. This is
	// correct: if the pruner is somehow restarted at exactly 02:00:00 local,
	// it should not immediately fire (would race with the previous run that
	// just woke up).
	now := time.Date(2026, 5, 9, 2, 0, 0, 0, time.Local)
	got := nextRunAt(now, 2)
	want := time.Date(2026, 5, 10, 2, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAuditLogPrunerOpts_Defaults(t *testing.T) {
	var opts AuditLogPrunerOpts
	opts.defaults()

	if opts.RetentionDuration != 365*24*time.Hour {
		t.Errorf("default RetentionDuration: want 1y, got %v", opts.RetentionDuration)
	}
	if opts.BatchSize != 1000 {
		t.Errorf("default BatchSize: want 1000, got %d", opts.BatchSize)
	}
	if opts.MaxBatches != 100 {
		t.Errorf("default MaxBatches: want 100, got %d", opts.MaxBatches)
	}
	if opts.Now == nil {
		t.Error("default Now must be non-nil")
	}
	if opts.Logger == nil {
		t.Error("default Logger must be non-nil")
	}
}
