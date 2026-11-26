// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// SetCondition sets or updates a condition on a slice of conditions.
//
// M6: the implementation lives in the CRD contract module
// (operators/api/v1.SetCondition); this is a thin re-export so the 40
// operator-internal callers keep using `conditions.SetCondition` unchanged.
func SetCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, observedGeneration int64) {
	apiv1.SetCondition(conditions, conditionType, status, reason, message, observedGeneration)
}

// GetCondition returns the condition with the given type, or nil if not found.
//
// M6: re-export of operators/api/v1.GetCondition (see SetCondition).
func GetCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	return apiv1.GetCondition(conditions, conditionType)
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
