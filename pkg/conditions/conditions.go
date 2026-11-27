// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package conditions provides kstatus-compatible condition contracts
// for LoopRuntime platform resources, without depending on k8s.io/apimachinery.
//
// Per ADR-0032 (introduced in MEET-2026-05-04-PKG-SOTA): pkg/conditions
// is the canonical replacement for the deleted pkg/health package. It
// defines a transport-agnostic Condition type that mirrors
// metav1.Condition's shape so callers (operators, future controllers,
// CLI inspectors) can share helpers like SetCondition, IsConditionTrue,
// and SetReadinessSummary without leaking apimachinery into pkg/.
//
// Operators can adopt these helpers two ways:
//
//  1. Direct use of conditions.Condition in a CRD (no Kubernetes deps).
//
//  2. Adapter pattern: convert a metav1.Condition slice to/from
//     conditions.Condition via the trivial 1:1 mappers in
//     operators/internal/conditions/adapter.go (added when adopted).
//     This keeps the operator status field metav1.Condition-typed for
//     kubectl/kstatus consumption while sharing the algorithmic core.
//
// Polarity (kstatus convention, kpt CRD Status Convention):
//
//   - Ready=True       → fully reconciled (consumers see "Current")
//   - Reconciling=True → controller in progress ("InProgress")
//   - Stalled=True     → manual intervention required ("Failed")
package conditions

import "time"

// Status mirrors metav1.ConditionStatus values without the apimachinery
// import. The string forms are identical so adapter conversion is
// lossless.
type Status string

const (
	// StatusTrue indicates the condition holds.
	StatusTrue Status = "True"
	// StatusFalse indicates the condition does not hold.
	StatusFalse Status = "False"
	// StatusUnknown indicates the controller cannot determine the
	// condition (often equivalent to Reconciling for kstatus consumers).
	StatusUnknown Status = "Unknown"
)

// Condition mirrors metav1.Condition without depending on apimachinery.
// All fields use stdlib types so this struct is portable to any binary
// that imports pkg/.
type Condition struct {
	// Type is the condition name (e.g., "Ready", "Reconciling").
	// Use the exported Type* constants where applicable.
	Type string

	// Status is True, False, or Unknown.
	Status Status

	// Reason is a CamelCase machine-readable token. Use the exported
	// Reason* constants where applicable.
	Reason string

	// Message is a human-readable description of the current state.
	Message string

	// LastTransitionTime is the time the condition last changed Status.
	// Preserved across reconciles when status is stable.
	LastTransitionTime time.Time

	// ObservedGeneration tracks which generation of the resource the
	// condition reflects. Required for kstatus to distinguish
	// "fully reconciled" from "spec changed; controller catching up".
	ObservedGeneration int64
}

// SetCondition sets or updates a condition on a slice. If a condition
// of the same Type already exists with the same Status, only Reason,
// Message, and ObservedGeneration are updated (LastTransitionTime is
// preserved). If Status changes, LastTransitionTime is set to the
// current time.
//
// Returns true if the condition was added or its Status changed; false
// if only metadata fields were updated. Useful for callers that want
// to skip a Status() patch when nothing material changed.
//
// Note: now() is used for time stamps. Tests can override via
// SetClock for determinism.
func SetCondition(conds *[]Condition, conditionType string, status Status, reason, message string, observedGeneration int64) bool {
	if conds == nil {
		return false
	}
	current := now()

	for i, c := range *conds {
		if c.Type != conditionType {
			continue
		}
		if c.Status == status {
			// Status unchanged — preserve transition time.
			(*conds)[i].Reason = reason
			(*conds)[i].Message = message
			(*conds)[i].ObservedGeneration = observedGeneration
			return false
		}
		// Status changed — update everything.
		(*conds)[i] = Condition{
			Type:               conditionType,
			Status:             status,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: current,
			ObservedGeneration: observedGeneration,
		}
		return true
	}

	*conds = append(*conds, Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: current,
		ObservedGeneration: observedGeneration,
	})
	return true
}

// GetCondition returns a pointer to the condition with the given Type,
// or nil if not found. The returned pointer references the slice;
// callers MUST treat it as read-only or risk drift.
func GetCondition(conds []Condition, conditionType string) *Condition {
	for i := range conds {
		if conds[i].Type == conditionType {
			return &conds[i]
		}
	}
	return nil
}

// IsConditionTrue returns true if a condition of the given Type exists
// with Status==True. Returns false for absent or non-True conditions.
func IsConditionTrue(conds []Condition, conditionType string) bool {
	c := GetCondition(conds, conditionType)
	return c != nil && c.Status == StatusTrue
}

// IsConditionTrueForGeneration returns true if a condition exists with
// Status==True AND its ObservedGeneration matches the given generation.
// Use this in controllers to gate "ready" decisions on the latest spec
// revision (kstatus discipline).
func IsConditionTrueForGeneration(conds []Condition, conditionType string, generation int64) bool {
	c := GetCondition(conds, conditionType)
	return c != nil && c.Status == StatusTrue && c.ObservedGeneration == generation
}

// AllConditionsTrue returns true iff every requested type exists with
// Status==True.
func AllConditionsTrue(conds []Condition, types ...string) bool {
	for _, t := range types {
		if !IsConditionTrue(conds, t) {
			return false
		}
	}
	return true
}

// --- Time injection for tests ---

// now returns the current time. Tests override via SetClock.
var now = time.Now

// SetClock replaces the package time source. Returns the previous
// clock so tests can restore it. Not safe for concurrent use across
// tests — call from t.Cleanup.
func SetClock(fn func() time.Time) (previous func() time.Time) {
	previous = now
	now = fn
	return previous
}
