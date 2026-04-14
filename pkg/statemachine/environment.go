// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Environment states as defined in the CRD (loopzero.io/v1 Environment).
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
//	Pending     --Provisioned-->     Active
//	Pending     --DeleteRequested--> Deleting
//	Active      --DeleteRequested--> Deleting
//
// Terminal: Deleting (finalizer removal completes object deletion)
func NewEnvironmentMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Environment",
		Transitions: []transition{
			{From: EnvironmentEmpty, Event: EnvironmentCreate, To: EnvironmentPending},
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
