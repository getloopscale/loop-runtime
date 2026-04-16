// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package health provides pure-Go health phase definitions and aggregation
// for the LoopZero platform. It has no Kubernetes dependencies and relies only
// on the Go standard library.
//
// Phases represent the lifecycle state of a platform resource. Aggregation
// uses temporal thresholds to prevent oscillation between states.
package health

import "fmt"

// Phase represents the lifecycle phase of a platform resource.
type Phase string

const (
	// PhasePending indicates the resource has been created but not yet reconciled.
	PhasePending Phase = "Pending"

	// PhaseProvisioning indicates the controller is actively reconciling the resource.
	PhaseProvisioning Phase = "Provisioning"

	// PhaseReady indicates all conditions are met and the resource is operational.
	PhaseReady Phase = "Ready"

	// PhaseDegraded indicates some conditions have not been met for longer than
	// the configured threshold. The resource is partially operational.
	PhaseDegraded Phase = "Degraded"

	// PhaseDeleting indicates the resource is marked for deletion and finalizers
	// are running. This is NOT a terminal phase -- the resource transitions to
	// gone (removed from the API server) after finalizers complete.
	PhaseDeleting Phase = "Deleting"

	// PhaseFailed indicates a terminal failure that requires manual intervention.
	// No automatic recovery is possible from this phase.
	PhaseFailed Phase = "Failed"
)

// allPhases is the canonical set of valid phases, defined once to avoid
// duplication between AllPhases and ValidatePhase.
var allPhases = []Phase{
	PhasePending,
	PhaseProvisioning,
	PhaseReady,
	PhaseDegraded,
	PhaseDeleting,
	PhaseFailed,
}

// AllPhases returns all valid Phase values.
func AllPhases() []Phase {
	result := make([]Phase, len(allPhases))
	copy(result, allPhases)
	return result
}

// IsTerminal returns true if the phase represents a terminal state from which
// no further automatic transitions are possible. Only PhaseFailed is terminal.
// PhaseDeleting is NOT terminal because it transitions to resource removal.
func IsTerminal(p Phase) bool {
	return p == PhaseFailed
}

// ValidatePhase returns an error if the given phase is not a recognized value.
func ValidatePhase(p Phase) error {
	for _, valid := range allPhases {
		if p == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid phase %q: must be one of %v", p, allPhases)
}
