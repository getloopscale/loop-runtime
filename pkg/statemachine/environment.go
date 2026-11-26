// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// Environment states as defined in the CRD (runtime.getloopscale.com/v1 Environment).
// EnvironmentEmpty represents the initial zero-value before first reconcile.
const (
	EnvironmentEmpty    State = ""
	EnvironmentPending  State = "Pending"
	EnvironmentActive   State = "Active"
	EnvironmentFailed   State = "Failed"
	EnvironmentDeleting State = "Deleting"
)

// Environment events that trigger state transitions.
const (
	EnvironmentCreate          Event = "Create"
	EnvironmentProvisioned     Event = "Provisioned"
	EnvironmentProvisionFailed Event = "ProvisionFailed"
	EnvironmentRetry           Event = "Retry"
	EnvironmentDeleteRequested Event = "DeleteRequested"
)

// NewEnvironmentMachine returns the Environment lifecycle state machine.
//
// Transitions:
//
//	"" (empty)  --Create-->          Pending
//	"" (empty)  --DeleteRequested--> Deleting
//	Pending     --Provisioned-->     Active
//	Pending     --ProvisionFailed--> Failed
//	Pending     --DeleteRequested--> Deleting
//	Failed      --Retry-->           Pending
//	Failed      --DeleteRequested--> Deleting
//	Active      --DeleteRequested--> Deleting
//
// Terminal: Deleting (finalizer removal completes object deletion).
// DeleteRequested is reachable from every non-terminal state so a delete that
// arrives before the first successful reconcile never deadlocks the finalizer
// (MEET-2026-07-02-002 MA-3).
func NewEnvironmentMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Environment",
		Transitions: []transition{
			{From: EnvironmentEmpty, Event: EnvironmentCreate, To: EnvironmentPending},
			// MEET-2026-07-02-002 MA-3: additive edge — delete from Empty.
			{From: EnvironmentEmpty, Event: EnvironmentDeleteRequested, To: EnvironmentDeleting},
			{From: EnvironmentPending, Event: EnvironmentProvisioned, To: EnvironmentActive},
			{From: EnvironmentPending, Event: EnvironmentProvisionFailed, To: EnvironmentFailed},
			{From: EnvironmentPending, Event: EnvironmentDeleteRequested, To: EnvironmentDeleting},
			{From: EnvironmentFailed, Event: EnvironmentRetry, To: EnvironmentPending},
			{From: EnvironmentFailed, Event: EnvironmentDeleteRequested, To: EnvironmentDeleting},
			{From: EnvironmentActive, Event: EnvironmentDeleteRequested, To: EnvironmentDeleting},
		},
		Terminal: []State{EnvironmentDeleting},
	})
}
