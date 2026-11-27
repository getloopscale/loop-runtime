// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
)

// allFourConditions returns conditions for the 4 tenant-scoped required types, all True.
// After Fase 3 refactoring, namespace/RBAC/NetworkPolicy conditions moved to Environment CRs.
func allFourConditions() []metav1.Condition {
	types := []string{
		conditions.TypeGitRepoReady,
		conditions.TypeArgoCDAppReady,
		conditions.TypeBuildNamespaceReady,
		conditions.TypeQuotaEnforced,
	}
	conds := make([]metav1.Condition, 0, len(types))
	for _, t := range types {
		conds = append(conds, metav1.Condition{
			Type:   t,
			Status: metav1.ConditionTrue,
		})
	}
	return conds
}

// allFiveConditions returns all 5 required conditions (including PriorityClassReady), all True.
func allFiveConditions() []metav1.Condition {
	conds := allFourConditions()
	conds = append(conds, metav1.Condition{
		Type:   conditions.TypePriorityClassReady,
		Status: metav1.ConditionTrue,
	})
	return conds
}

func TestAllConditionsTrue_RequiresPriorityClassReady(t *testing.T) {
	r := &TenantReconciler{}

	t.Run("returns false when PriorityClassReady is missing", func(t *testing.T) {
		conds := allFourConditions()
		result := conditions.AllConditionsTrue(conds, r.requiredProvisioningConditions()...)
		if result {
			t.Error("allConditionsTrue should return false when PriorityClassReady is missing")
		}
	})

	t.Run("returns false when PriorityClassReady is False", func(t *testing.T) {
		conds := allFourConditions()
		conds = append(conds, metav1.Condition{
			Type:   conditions.TypePriorityClassReady,
			Status: metav1.ConditionFalse,
		})
		result := conditions.AllConditionsTrue(conds, r.requiredProvisioningConditions()...)
		if result {
			t.Error("allConditionsTrue should return false when PriorityClassReady is False")
		}
	})

	t.Run("returns true when all 5 conditions including PriorityClassReady are True", func(t *testing.T) {
		conds := allFiveConditions()
		result := conditions.AllConditionsTrue(conds, r.requiredProvisioningConditions()...)
		if !result {
			t.Error("allConditionsTrue should return true when all 5 conditions are True")
		}
	})
}
