// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package health

import "time"

// ConditionInput represents a single health condition to be aggregated.
type ConditionInput struct {
	// Name identifies the condition (e.g., "GiteaReady", "ArgoReady").
	Name string

	// Healthy is true when the condition is met.
	Healthy bool

	// Critical indicates whether this condition has a shorter tolerance
	// threshold before the system is considered Degraded.
	Critical bool

	// Since is the timestamp when the condition entered its current state.
	// Used with the aggregator thresholds to prevent oscillation.
	Since time.Time
}

// AggregatorConfig controls the temporal thresholds for health aggregation.
// Conditions must be unhealthy for longer than their threshold before
// the aggregated phase transitions to Degraded.
type AggregatorConfig struct {
	// CriticalThreshold is how long a critical condition can be unhealthy
	// before the aggregated phase becomes Degraded.
	CriticalThreshold time.Duration

	// NonCriticalThreshold is how long a non-critical condition can be
	// unhealthy before the aggregated phase becomes Degraded.
	NonCriticalThreshold time.Duration
}

// DefaultAggregatorConfig returns an AggregatorConfig with the standard
// thresholds: 10s for critical conditions, 60s for non-critical.
func DefaultAggregatorConfig() AggregatorConfig {
	return AggregatorConfig{
		CriticalThreshold:    10 * time.Second,
		NonCriticalThreshold: 60 * time.Second,
	}
}

// Aggregate computes the overall Phase from a set of conditions.
//
// Rules:
//   - No conditions: Pending (nothing to evaluate)
//   - All healthy: Ready
//   - Any unhealthy for less than its threshold: Provisioning (still converging)
//   - Any unhealthy for >= its threshold: Degraded
//
// The now parameter is accepted explicitly for testability. Callers should
// pass time.Now() in production code.
func Aggregate(conditions []ConditionInput, cfg AggregatorConfig, now time.Time) Phase {
	if len(conditions) == 0 {
		return PhasePending
	}

	anyUnhealthy := false
	anyExceededThreshold := false

	for _, c := range conditions {
		if c.Healthy {
			continue
		}

		anyUnhealthy = true

		threshold := cfg.NonCriticalThreshold
		if c.Critical {
			threshold = cfg.CriticalThreshold
		}

		unhealthyDuration := now.Sub(c.Since)
		if unhealthyDuration >= threshold {
			anyExceededThreshold = true
			break // no need to check further, Degraded is the worst non-terminal outcome
		}
	}

	if !anyUnhealthy {
		return PhaseReady
	}

	if anyExceededThreshold {
		return PhaseDegraded
	}

	return PhaseProvisioning
}
