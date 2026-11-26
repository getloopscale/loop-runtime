// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

type fakeStaleBuildRows struct {
	cutoffs store.StaleBuildCutoffs
	err     error
}

func (f *fakeStaleBuildRows) StaleBuildRows(_ context.Context, _ string, cutoffs store.StaleBuildCutoffs) ([]store.StaleBuildRow, error) {
	f.cutoffs = cutoffs
	return nil, f.err
}

func TestStaleBuildChecker_CutoffsAreNowMinusEachPhaseTimeout(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	rows := &fakeStaleBuildRows{}
	checker := &staleBuildChecker{rows: rows, now: func() time.Time { return now }}

	if _, err := checker.FindStaleBuilds(context.Background(), "", time.Minute, 2*time.Minute, 3*time.Minute, 4*time.Minute); err != nil {
		t.Fatalf("FindStaleBuilds: %v", err)
	}

	want := store.StaleBuildCutoffs{
		QueuedBefore: now.Add(-time.Minute), RunningBefore: now.Add(-2 * time.Minute),
		ReleasingBefore: now.Add(-3 * time.Minute), CancellingBefore: now.Add(-4 * time.Minute),
	}
	if rows.cutoffs != want {
		t.Errorf("cutoffs = %+v, want %+v", rows.cutoffs, want)
	}
}

func TestStaleBuildChecker_QueryErrorPropagates(t *testing.T) {
	boom := errors.New("db down")
	checker := NewStaleBuildChecker(&fakeStaleBuildRows{err: boom})

	_, err := checker.FindStaleBuilds(context.Background(), "", time.Minute, time.Minute, time.Minute, time.Minute)

	if !errors.Is(err, boom) {
		t.Fatalf("expected the query error, got %v", err)
	}
}
