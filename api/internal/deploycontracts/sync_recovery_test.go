// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// scriptedRecoveryClient answers TriggerSync from a script, one error per call (nil = success),
// and records the order of calls so a test can see where termination happened.
type scriptedRecoveryClient struct {
	triggerErrs  []error
	terminateErr error
	calls        []string
}

func (s *scriptedRecoveryClient) TriggerSync(_ context.Context, _ string) error {
	s.calls = append(s.calls, "trigger")
	if len(s.triggerErrs) == 0 {
		return nil
	}
	err := s.triggerErrs[0]
	s.triggerErrs = s.triggerErrs[1:]
	return err
}

func (s *scriptedRecoveryClient) TerminateOperation(_ context.Context, _ string) error {
	s.calls = append(s.calls, "terminate")
	return s.terminateErr
}

var errStuck = fmt.Errorf("wrapped: %w", loopzeroerrors.ErrSyncOperationInProgress)

func silentLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// recordWaits returns a wait function that records each backoff instead of sleeping.
func recordRecoveryWaits(into *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*into = append(*into, d)
		return nil
	}
}

func linearBackoff(d time.Duration) func(int, bool) time.Duration {
	return func(int, bool) time.Duration { return d }
}

func TestTrigger_ReturnsNilOnFirstSuccess(t *testing.T) {
	s := &scriptedRecoveryClient{}
	var waits []time.Duration

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 3, MaxStuckAttempts: 3, Backoff: linearBackoff(time.Second)}, silentLogger(), recordRecoveryWaits(&waits))

	if err != nil || len(s.calls) != 1 || len(waits) != 0 {
		t.Fatalf("err=%v calls=%v waits=%v", err, s.calls, waits)
	}
}

func TestTrigger_NonStuckFailureStopsAtMaxAttempts(t *testing.T) {
	boom := errors.New("connection refused")
	s := &scriptedRecoveryClient{triggerErrs: []error{boom, boom, boom, boom}}
	var waits []time.Duration

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 3, MaxStuckAttempts: 8, TerminateAfterStuck: 1, Backoff: linearBackoff(time.Second)}, silentLogger(), recordRecoveryWaits(&waits))

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
	s := &scriptedRecoveryClient{triggerErrs: []error{errStuck, errStuck, errStuck, errStuck, errStuck}}
	var waits []time.Duration

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 2, MaxStuckAttempts: 4, Backoff: linearBackoff(time.Second)}, silentLogger(), recordRecoveryWaits(&waits))

	if !IsSyncStuck(err) {
		t.Fatalf("want stuck error, got %v", err)
	}
	if got := len(s.calls); got != 4 {
		t.Errorf("want 4 trigger attempts under the stuck budget, got %d", got)
	}
}

func TestTrigger_TerminatesOnceConsecutiveStuckReachesThreshold(t *testing.T) {
	s := &scriptedRecoveryClient{triggerErrs: []error{errStuck, errStuck, nil}}
	var waits []time.Duration

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 3, MaxStuckAttempts: 3, TerminateAfterStuck: 2, Backoff: linearBackoff(time.Second)}, silentLogger(), recordRecoveryWaits(&waits))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"trigger", "trigger", "terminate", "trigger"}
	if fmt.Sprint(s.calls) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", s.calls, want)
	}
}

func TestTrigger_DoesNotTerminateOnANonStuckFailure(t *testing.T) {
	s := &scriptedRecoveryClient{triggerErrs: []error{errors.New("503"), nil}}
	var waits []time.Duration

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 3, MaxStuckAttempts: 3, TerminateAfterStuck: 1, Backoff: linearBackoff(time.Second)}, silentLogger(), recordRecoveryWaits(&waits))

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
	s := &scriptedRecoveryClient{triggerErrs: []error{errStuck, nil}, terminateErr: errors.New("403")}
	var waits []time.Duration

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 2, MaxStuckAttempts: 2, TerminateAfterStuck: 1, Backoff: linearBackoff(time.Second)}, silentLogger(), recordRecoveryWaits(&waits))

	if err != nil {
		t.Fatalf("the retry after a failed termination decides the outcome; got %v", err)
	}
}

func TestTrigger_PassesFailureCountAndKindToBackoff(t *testing.T) {
	s := &scriptedRecoveryClient{triggerErrs: []error{errors.New("x"), errStuck, nil}}
	var waits []time.Duration
	backoff := func(failures int, stuck bool) time.Duration {
		if stuck {
			return time.Duration(100+failures) * time.Millisecond
		}
		return time.Duration(failures) * time.Millisecond
	}

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 5, MaxStuckAttempts: 5, Backoff: backoff}, silentLogger(), recordRecoveryWaits(&waits))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []time.Duration{1 * time.Millisecond, 102 * time.Millisecond}
	if fmt.Sprint(waits) != fmt.Sprint(want) {
		t.Errorf("waits = %v, want %v", waits, want)
	}
}

func TestTrigger_ReturnsContextErrorWhenCancelledDuringBackoff(t *testing.T) {
	s := &scriptedRecoveryClient{triggerErrs: []error{errors.New("x"), nil}}
	cancelled := func(context.Context, time.Duration) error { return context.Canceled }

	err := triggerSyncWithRecovery(context.Background(), s, "app", SyncRecoveryPolicy{MaxAttempts: 3, MaxStuckAttempts: 3, Backoff: linearBackoff(time.Second)}, silentLogger(), cancelled)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestSleep_ReturnsContextErrorWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sleepOrDone(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestTerminate_ReportsWhetherTheOperationWasTerminated(t *testing.T) {
	if !TerminateStuckSync(context.Background(), &scriptedRecoveryClient{}, "app", silentLogger()) {
		t.Error("a successful termination must report true")
	}
	if TerminateStuckSync(context.Background(), &scriptedRecoveryClient{terminateErr: errors.New("403")}, "app", silentLogger()) {
		t.Error("a failed termination must report false")
	}
}

func TestShouldTerminate_ZeroThresholdNeverTerminates(t *testing.T) {
	if (SyncRecoveryPolicy{}).ShouldTerminate(10) {
		t.Error("a policy with no termination threshold must never terminate")
	}
}
