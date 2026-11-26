// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// Beta Logs Gate Criterion 6 — timeout reasons must include:
//   - The SM state name (QUEUED / RUNNING / RELEASING / cancellation)
//   - A "Likely cause:" phrase
//   - A "Next:" actionable hint
//
// The case-insensitive checks below capture the contract without
// pinning the exact wording (so we can keep tightening the messages
// without test churn).

func TestReaperReason_QueueTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BPEventQueueTimeout, DefaultBuildReaperConfig())
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "queued") {
		t.Errorf("expected QUEUED state mention, got %q", reason)
	}
	if !strings.Contains(lower, "5 minute") {
		t.Errorf("expected budget mention (5 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase (Beta Logs Gate Criterion 6), got %q", reason)
	}
	if !strings.Contains(lower, "next:") {
		t.Errorf("expected `Next:` actionable hint, got %q", reason)
	}
}

func TestReaperReason_BuildTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BPEventTimeout, DefaultBuildReaperConfig())
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "running") {
		t.Errorf("expected RUNNING state mention, got %q", reason)
	}
	if !strings.Contains(lower, "30 minute") {
		t.Errorf("expected budget mention (30 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase, got %q", reason)
	}
	if !strings.Contains(lower, "loopzero logs") {
		t.Errorf("expected `loopzero logs` next-step hint, got %q", reason)
	}
}

func TestReaperReason_ReleasingTimeout(t *testing.T) {
	// Beta Logs Gate gap closed by iter 6 — the RELEASING state had
	// no dedicated reason branch (fell through to "Build reaped due
	// to timeout"). Now produces an actionable RELEASING-specific
	// message.
	reason := reaperReason(statemachine.BPEventReleaseTimeout, DefaultBuildReaperConfig())
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "releasing") {
		t.Errorf("expected RELEASING state mention, got %q", reason)
	}
	if !strings.Contains(lower, "10 minute") {
		t.Errorf("expected budget mention (10 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase, got %q", reason)
	}
}

func TestReaperReason_CancelTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BPEventCancelTimeout, DefaultBuildReaperConfig())
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "cancellation") {
		t.Errorf("expected cancellation mention, got %q", reason)
	}
	if !strings.Contains(lower, "60s") {
		t.Errorf("expected budget mention (60s), got %q", reason)
	}
	if !strings.Contains(lower, "next:") {
		t.Errorf("expected `Next:` actionable hint, got %q", reason)
	}
}

func TestReaperReason_UnknownEvent(t *testing.T) {
	reason := reaperReason(statemachine.BuildPhaseEvent("some_unknown_event"), DefaultBuildReaperConfig())
	if !strings.Contains(strings.ToLower(reason), "timeout") {
		t.Errorf("expected generic timeout reason, got %q", reason)
	}
	// Even the fallback must still point at a CLI command.
	if !strings.Contains(strings.ToLower(reason), "loopzero logs") {
		t.Errorf("fallback must still suggest `loopzero logs`, got %q", reason)
	}
}

// The reason text names the budget the reaper actually applied. The durations
// are configurable (BUILD_*_TIMEOUT), and a message hard-coding the defaults
// misreports every reap in a deployment that changed them.
func TestReaperReason_NamesTheConfiguredBudget(t *testing.T) {
	cfg := DefaultBuildReaperConfig()
	cfg.QueueTimeout = 7 * time.Minute

	reason := reaperReason(statemachine.BPEventQueueTimeout, cfg)

	if !strings.Contains(reason, "7 minutes") {
		t.Errorf("reason must name the configured 7-minute budget, got %q", reason)
	}
}

// Every build state the machine gives a timeout has a reaper timeout event, and
// that event is a legal edge out of the state — so the reaper and the machine
// cannot disagree about which states time out or how.
func TestBuildTimeoutEvents_MatchTheMachineTimeouts(t *testing.T) {
	m := statemachine.NewBuildTypedMachine()
	for _, tc := range m.Timeouts() {
		phase := statemachine.BuildPhase(tc.State)
		event, ok := buildTimeoutEvents[phase]
		if !ok {
			t.Errorf("state %s has a machine timeout but no reaper timeout event", phase)
			continue
		}
		if _, err := m.TryTransition(phase, event); err != nil {
			t.Errorf("timeout event %s is not a legal edge out of %s: %v", event, phase, err)
		}
	}
	if len(buildTimeoutEvents) != len(m.Timeouts()) {
		t.Errorf("reaper times out %d states, the machine %d", len(buildTimeoutEvents), len(m.Timeouts()))
	}
}

func TestDefaultBuildReaperConfig_UsesTheMachineTimeouts(t *testing.T) {
	cfg := DefaultBuildReaperConfig()
	want := map[statemachine.BuildPhase]time.Duration{}
	for _, tc := range statemachine.NewBuildTypedMachine().Timeouts() {
		want[statemachine.BuildPhase(tc.State)] = tc.Timeout
	}
	got := map[statemachine.BuildPhase]time.Duration{
		statemachine.BPhQueued: cfg.QueueTimeout, statemachine.BPhRunning: cfg.RunningTimeout,
		statemachine.BPhReleasing: cfg.ReleasingTimeout, statemachine.BPhCancelling: cfg.CancelTimeout,
	}
	for phase, d := range want {
		if got[phase] != d {
			t.Errorf("%s default = %v, machine timeout = %v", phase, got[phase], d)
		}
	}
}
