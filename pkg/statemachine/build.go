package statemachine

import "time"

// Build states as defined in spec section 5.1.
const (
	BuildQueued     State = "QUEUED"
	BuildRunning    State = "RUNNING"
	BuildSucceeded  State = "SUCCEEDED"
	BuildFailed     State = "FAILED"
	BuildCancelling State = "CANCELLING"
	BuildCancelled  State = "CANCELLED"
)

// Build state timeout defaults (spec §5.1).
const (
	BuildQueuedTimeout     = 5 * time.Minute
	BuildRunningTimeout    = 30 * time.Minute
	BuildCancellingTimeout = 60 * time.Second
)

// Build events as defined in spec section 5.1.
const (
	BuildStarted          Event = "BuildStarted"
	BuildSucceededEvent   Event = "BuildSucceeded"
	BuildFailedEvent      Event = "BuildFailed"
	BuildCancelRequested  Event = "BuildCancelRequested"
	BuildCancelConfirmed  Event = "BuildCancelConfirmed"
	BuildQueueTimeout     Event = "BuildQueueTimeout"
	BuildTimeout          Event = "BuildTimeout"
	BuildCancelTimeout    Event = "BuildCancelTimeout"
)

// IsBuildTerminal returns true if the given state is a terminal build state
// (SUCCEEDED, FAILED, or CANCELLED). This is a standalone helper that avoids
// requiring a Machine instance, useful in places like SSE stream handlers and
// orchestrators that only need a quick terminal-state check.
func IsBuildTerminal(state State) bool {
	switch state {
	case BuildSucceeded, BuildFailed, BuildCancelled:
		return true
	}
	return false
}

// NewBuildMachine returns the build state machine per spec section 5.1.
//
// States: QUEUED -> RUNNING -> SUCCEEDED|FAILED|CANCELLING -> CANCELLED
// Terminal: SUCCEEDED, FAILED, CANCELLED
// Timeouts: QUEUED 5min, RUNNING 30min, CANCELLING 60s
func NewBuildMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Build",
		Transitions: []transition{
			{From: BuildQueued, Event: BuildStarted, To: BuildRunning},
			{From: BuildQueued, Event: BuildCancelRequested, To: BuildCancelled},
			{From: BuildQueued, Event: BuildQueueTimeout, To: BuildFailed},
			{From: BuildRunning, Event: BuildSucceededEvent, To: BuildSucceeded},
			{From: BuildRunning, Event: BuildFailedEvent, To: BuildFailed},
			{From: BuildRunning, Event: BuildCancelRequested, To: BuildCancelling},
			{From: BuildRunning, Event: BuildTimeout, To: BuildFailed},
			{From: BuildCancelling, Event: BuildCancelConfirmed, To: BuildCancelled},
			{From: BuildCancelling, Event: BuildCancelTimeout, To: BuildCancelled},
		},
		Terminal: []State{BuildSucceeded, BuildFailed, BuildCancelled},
		Timeouts: []TimeoutConfig{
			{State: BuildQueued, Timeout: BuildQueuedTimeout, Reason: "QUEUE_TIMEOUT"},
			{State: BuildRunning, Timeout: BuildRunningTimeout, Reason: "BUILD_TIMEOUT"},
			{State: BuildCancelling, Timeout: BuildCancellingTimeout, Reason: "CANCEL_TIMEOUT"},
		},
	})
}
