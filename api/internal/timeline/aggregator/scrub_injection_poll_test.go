// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package aggregator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/scrub"
)

// The scrub the aggregator is constructed with is the one it applies; a test
// injecting a different one must see it take effect on phase events.
func TestAggregatorAppliesTheInjectedScrub(t *testing.T) {
	// Arrange
	store := &memoryStore{}
	reader := &scriptedReader{id: SourceReleaseDB, events: []PhaseEvent{
		{Phase: "build", Status: "degraded", DegradedReason: "raw internal detail", Timestamp: now()},
	}}
	marker := func(in scrub.PhaseEvent) (scrub.PhaseEvent, int) {
		in.DegradedReason = "scrubbed-by-injection"
		return in, 1
	}
	agg := New("dep-inject", store, marker, []SourceReader{reader})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Act
	agg.Run(ctx)

	// Assert
	got := store.snapshot()
	if len(got) == 0 || !strings.Contains(string(got[0].Data), "scrubbed-by-injection") {
		t.Fatalf("injected scrub not applied; events = %+v", got)
	}
}

func TestPollUntilTerminalStopsAtTheFirstTerminalEmit(t *testing.T) {
	calls := 0
	err := pollUntilTerminal(context.Background(), time.Millisecond, func() (bool, error) {
		calls++
		return calls == 3, nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d, want nil and 3", err, calls)
	}
}

func TestPollUntilTerminalReturnsTheEmitError(t *testing.T) {
	boom := errors.New("query failed")
	err := pollUntilTerminal(context.Background(), time.Millisecond, func() (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestPollUntilTerminalReturnsTheContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pollUntilTerminal(ctx, time.Hour, func() (bool, error) { return false, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
