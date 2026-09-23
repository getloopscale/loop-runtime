// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package syncrecovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// scriptedSyncer answers TriggerSync from a script, one error per call (nil = success),
// and records the order of calls so a test can see where termination happened.
type scriptedSyncer struct {
	triggerErrs  []error
	terminateErr error
	calls        []string
}

func (s *scriptedSyncer) TriggerSync(_ context.Context, _ string) error {
	s.calls = append(s.calls, "trigger")
	if len(s.triggerErrs) == 0 {
		return nil
	}
	err := s.triggerErrs[0]
	s.triggerErrs = s.triggerErrs[1:]
	return err
}

func (s *scriptedSyncer) TerminateOperation(_ context.Context, _ string) error {
	s.calls = append(s.calls, "terminate")
	return s.terminateErr
}

var stuck = fmt.Errorf("wrapped: %w", loopzeroerrors.ErrSyncOperationInProgress)

func silent() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// recordWaits returns a wait function that records each backoff instead of sleeping.
func recordWaits(into *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*into = append(*into, d)
		return nil
	}
}

func linear(d time.Duration) func(int, bool) time.Duration {
	return func(int, bool) time.Duration { return d }
}

func TestTrigger_ReturnsNilOnFirstSuccess(t *testing.T) {
	s := &scriptedSyncer{}
	var waits []time.Duration

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 3, MaxStuckAttempts: 3, Backoff: linear(time.Second)}, silent(), recordWaits(&waits))

	if err != nil || len(s.calls) != 1 || len(waits) != 0 {
		t.Fatalf("err=%v calls=%v waits=%v", err, s.calls, waits)
	}
}

func TestTrigger_NonStuckFailureStopsAtMaxAttempts(t *testing.T) {
	boom := errors.New("connection refused")
	s := &scriptedSyncer{triggerErrs: []error{boom, boom, boom, boom}}
	var waits []time.Duration

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 3, MaxStuckAttempts: 8, TerminateAfterStuck: 1, Backoff: linear(time.Second)}, silent(), recordWaits(&waits))

	if !errors.Is(err, boom) {
		t.Fatalf("want last trigger error, got %v", err)
	}
	if got := len(s.calls); got != 3 {
		t.Errorf("want 3 trigger attempts, got %d (%v)", got, s.calls)
	}
	if len(waits) != 2 {
		t.Errorf("want 2 backoffs between 3 attempts, got %v", waits)
	}
}

func TestTrigger_StuckFailureUsesTheStuckBudget(t *testing.T) {
	s := &scriptedSyncer{triggerErrs: []error{stuck, stuck, stuck, stuck, stuck}}
	var waits []time.Duration

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 2, MaxStuckAttempts: 4, Backoff: linear(time.Second)}, silent(), recordWaits(&waits))

	if !IsStuck(err) {
		t.Fatalf("want stuck error, got %v", err)
	}
	if got := len(s.calls); got != 4 {
		t.Errorf("want 4 trigger attempts under the stuck budget, got %d", got)
	}
}

func TestTrigger_TerminatesOnceConsecutiveStuckReachesThreshold(t *testing.T) {
	s := &scriptedSyncer{triggerErrs: []error{stuck, stuck, nil}}
	var waits []time.Duration

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 3, MaxStuckAttempts: 3, TerminateAfterStuck: 2, Backoff: linear(time.Second)}, silent(), recordWaits(&waits))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"trigger", "trigger", "terminate", "trigger"}
	if fmt.Sprint(s.calls) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", s.calls, want)
	}
}

func TestTrigger_DoesNotTerminateOnANonStuckFailure(t *testing.T) {
	s := &scriptedSyncer{triggerErrs: []error{errors.New("503"), nil}}
	var waits []time.Duration

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 3, MaxStuckAttempts: 3, TerminateAfterStuck: 1, Backoff: linear(time.Second)}, silent(), recordWaits(&waits))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range s.calls {
		if c == "terminate" {
			t.Fatalf("a non-stuck failure must not terminate the operation; calls=%v", s.calls)
		}
	}
}

func TestTrigger_RetriesAfterAFailedTermination(t *testing.T) {
	s := &scriptedSyncer{triggerErrs: []error{stuck, nil}, terminateErr: errors.New("403")}
	var waits []time.Duration

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 2, MaxStuckAttempts: 2, TerminateAfterStuck: 1, Backoff: linear(time.Second)}, silent(), recordWaits(&waits))

	if err != nil {
		t.Fatalf("the retry after a failed termination decides the outcome; got %v", err)
	}
}

func TestTrigger_PassesFailureCountAndKindToBackoff(t *testing.T) {
	s := &scriptedSyncer{triggerErrs: []error{errors.New("x"), stuck, nil}}
	var waits []time.Duration
	backoff := func(failures int, stuck bool) time.Duration {
		if stuck {
			return time.Duration(100+failures) * time.Millisecond
		}
		return time.Duration(failures) * time.Millisecond
	}

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 5, MaxStuckAttempts: 5, Backoff: backoff}, silent(), recordWaits(&waits))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []time.Duration{1 * time.Millisecond, 102 * time.Millisecond}
	if fmt.Sprint(waits) != fmt.Sprint(want) {
		t.Errorf("waits = %v, want %v", waits, want)
	}
}

func TestTrigger_ReturnsContextErrorWhenCancelledDuringBackoff(t *testing.T) {
	s := &scriptedSyncer{triggerErrs: []error{errors.New("x"), nil}}
	cancelled := func(context.Context, time.Duration) error { return context.Canceled }

	err := trigger(context.Background(), s, "app", Policy{MaxAttempts: 3, MaxStuckAttempts: 3, Backoff: linear(time.Second)}, silent(), cancelled)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestSleep_ReturnsContextErrorWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestTerminate_ReportsWhetherTheOperationWasTerminated(t *testing.T) {
	if !Terminate(context.Background(), &scriptedSyncer{}, "app", silent()) {
		t.Error("a successful termination must report true")
	}
	if Terminate(context.Background(), &scriptedSyncer{terminateErr: errors.New("403")}, "app", silent()) {
		t.Error("a failed termination must report false")
	}
}

func TestShouldTerminate_ZeroThresholdNeverTerminates(t *testing.T) {
	if (Policy{}).ShouldTerminate(10) {
		t.Error("a policy with no termination threshold must never terminate")
	}
}
