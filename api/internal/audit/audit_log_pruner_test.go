// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package audit

import (
	"context"
	"errors"
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

// scriptedDeleter returns the scripted batch sizes in order, then errAt's error.
type scriptedDeleter struct {
	batches []int64
	errAt   int
	err     error
	calls   int
	cutoff  time.Time
	limit   int
}

func (d *scriptedDeleter) DeleteAuditLogBatch(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	d.cutoff, d.limit = cutoff, limit
	i := d.calls
	d.calls++
	if d.err != nil && i == d.errAt {
		return 0, d.err
	}
	if i < len(d.batches) {
		return d.batches[i], nil
	}
	return 0, nil
}

func TestAuditLogPruner_StopsAtTheFirstUnderfilledBatch(t *testing.T) {
	now := time.Date(2026, 9, 25, 2, 0, 0, 0, time.UTC)
	d := &scriptedDeleter{batches: []int64{10, 10, 3, 10}}
	p := NewAuditLogPruner(d, AuditLogPrunerOpts{BatchSize: 10, RetentionDuration: 24 * time.Hour, Now: func() time.Time { return now }})

	total, err := p.PruneOnce(context.Background())

	if err != nil || total != 23 || d.calls != 3 {
		t.Fatalf("total=%d calls=%d err=%v, want 23 over 3 batches", total, d.calls, err)
	}
	if !d.cutoff.Equal(now.Add(-24*time.Hour)) || d.limit != 10 {
		t.Errorf("cutoff=%v limit=%d", d.cutoff, d.limit)
	}
}

func TestAuditLogPruner_CapsAPassAtMaxBatches(t *testing.T) {
	d := &scriptedDeleter{batches: []int64{5, 5, 5, 5}}
	p := NewAuditLogPruner(d, AuditLogPrunerOpts{BatchSize: 5, MaxBatches: 2})

	total, err := p.PruneOnce(context.Background())

	if err != nil || total != 10 || d.calls != 2 {
		t.Fatalf("total=%d calls=%d err=%v, want 10 over 2 batches", total, d.calls, err)
	}
}

func TestAuditLogPruner_BatchFailureStopsAndReportsWhatWasDeleted(t *testing.T) {
	boom := errors.New("trigger rejected delete")
	d := &scriptedDeleter{batches: []int64{5}, errAt: 1, err: boom}
	p := NewAuditLogPruner(d, AuditLogPrunerOpts{BatchSize: 5})

	total, err := p.PruneOnce(context.Background())

	if !errors.Is(err, boom) || total != 5 {
		t.Fatalf("total=%d err=%v, want 5 and the batch error", total, err)
	}
}
