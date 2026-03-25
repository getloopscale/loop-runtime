// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package health

import "time"

// ComponentHealth represents the health of a single platform component
// (e.g., "bootstrap", "gitops", "runtime").
type ComponentHealth struct {
	// Name identifies the component.
	Name string

	// Phase is the current health phase of this component.
	Phase Phase

	// Detail is a human-readable description of the component's state.
	Detail string
}

// PlatformHealth is the aggregated health of the entire platform,
// composed of individual component health reports.
type PlatformHealth struct {
	// Overall is the worst phase across all components.
	Overall Phase

	// Components lists the individual component health reports.
	Components []ComponentHealth

	// UpdatedAt is the timestamp when this aggregation was computed.
	UpdatedAt time.Time
}

// phaseSeverity maps each phase to a numeric severity for worst-phase
// comparison. Higher values indicate worse health.
var phaseSeverity = map[Phase]int{
	PhaseReady:        0,
	PhaseProvisioning: 1,
	PhasePending:      2,
	PhaseDegraded:     3,
	PhaseDeleting:     4,
	PhaseFailed:       5,
}

// AggregatePlatform computes the overall platform health from individual
// component health reports. The overall phase is the worst (highest severity)
// phase among all components.
//
// If no components are provided, the overall phase is Pending.
func AggregatePlatform(components []ComponentHealth) PlatformHealth {
	if len(components) == 0 {
		return PlatformHealth{
			Overall:    PhasePending,
			Components: []ComponentHealth{},
			UpdatedAt:  time.Now(),
		}
	}

	worst := PhaseReady
	worstSeverity := phaseSeverity[PhaseReady]

	for _, c := range components {
		severity, ok := phaseSeverity[c.Phase]
		if !ok {
			// Unknown phase treated as maximum severity to fail visibly.
			severity = len(phaseSeverity)
		}
		if severity > worstSeverity {
			worst = c.Phase
			worstSeverity = severity
		}
	}

	// Copy components to avoid caller mutation.
	copied := make([]ComponentHealth, len(components))
	copy(copied, components)

	return PlatformHealth{
		Overall:    worst,
		Components: copied,
		UpdatedAt:  time.Now(),
	}
}
