// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

type fakeStaleReleaseRows struct {
	readyBefore, pendingBefore time.Time
	err                        error
}

func (f *fakeStaleReleaseRows) StaleReleaseRows(_ context.Context, _ string, readyBefore, pendingBefore time.Time) ([]store.StaleReleaseRow, error) {
	f.readyBefore, f.pendingBefore = readyBefore, pendingBefore
	return nil, f.err
}

func TestStaleReleaseChecker_CutoffsAreNowMinusEachTimeout(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	rows := &fakeStaleReleaseRows{}
	checker := &staleReleaseChecker{rows: rows, now: func() time.Time { return now }}

	if _, err := checker.FindStaleReleases(context.Background(), "cell-1", 5*time.Minute, 10*time.Minute); err != nil {
		t.Fatalf("FindStaleReleases: %v", err)
	}

	if !rows.readyBefore.Equal(now.Add(-5*time.Minute)) || !rows.pendingBefore.Equal(now.Add(-10*time.Minute)) {
		t.Errorf("cutoffs = (%v, %v)", rows.readyBefore, rows.pendingBefore)
	}
}

func TestStaleReleaseChecker_QueryErrorPropagates(t *testing.T) {
	boom := errors.New("db down")
	checker := NewStaleReleaseChecker(&fakeStaleReleaseRows{err: boom})

	_, err := checker.FindStaleReleases(context.Background(), "", time.Minute, time.Minute)

	if !errors.Is(err, boom) {
		t.Fatalf("expected the query error, got %v", err)
	}
}
