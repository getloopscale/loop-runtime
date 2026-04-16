// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetCondition sets or updates a condition on a slice of conditions.
// ObservedGeneration is mandatory and tracks which generation of the resource
// the condition relates to. If the condition already exists with the same
// status, only reason, message, and observedGeneration are updated
// (preserving LastTransitionTime). If the status changes, LastTransitionTime
// is updated to the current time.
func SetCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, observedGeneration int64) {
	now := metav1.Now()

	for i, c := range *conditions {
		if c.Type == conditionType {
			if c.Status == status {
				// Status unchanged: update reason/message/generation but preserve transition time.
				(*conditions)[i].Reason = reason
				(*conditions)[i].Message = message
				(*conditions)[i].ObservedGeneration = observedGeneration
				return
			}
			// Status changed: update everything including transition time.
			(*conditions)[i] = metav1.Condition{
				Type:               conditionType,
				Status:             status,
				LastTransitionTime: now,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: observedGeneration,
			}
			return
		}
	}

	// Condition not found: append new.
	*conditions = append(*conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
	})
}

// GetCondition returns the condition with the given type, or nil if not found.
func GetCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// IsConditionTrue returns true if the condition exists and has status True.
func IsConditionTrue(conditions []metav1.Condition, conditionType string) bool {
	c := GetCondition(conditions, conditionType)
	return c != nil && c.Status == metav1.ConditionTrue
}

// IsConditionTrueForGeneration returns true if the condition exists, has
// status True, AND its ObservedGeneration matches the given generation.
// This prevents stale condition reads after spec changes.
func IsConditionTrueForGeneration(conditions []metav1.Condition, conditionType string, generation int64) bool {
	c := GetCondition(conditions, conditionType)
	return c != nil && c.Status == metav1.ConditionTrue && c.ObservedGeneration == generation
}

// AllConditionsTrue returns true if ALL specified condition types exist
// and have status True.
func AllConditionsTrue(conditions []metav1.Condition, types ...string) bool {
	for _, t := range types {
		if !IsConditionTrue(conditions, t) {
			return false
		}
	}
	return true
}
