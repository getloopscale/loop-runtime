// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/harbor"
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

// allSixConditions returns all 6 required conditions (including HarborProjectReady), all True.
func allSixConditions() []metav1.Condition {
	conds := allFiveConditions()
	conds = append(conds, metav1.Condition{
		Type:   conditions.TypeHarborProjectReady,
		Status: metav1.ConditionTrue,
	})
	return conds
}

// reconcilerWithoutHarbor returns a TenantReconciler with HarborClient=nil (DOCR mode).
func reconcilerWithoutHarbor() *TenantReconciler {
	return &TenantReconciler{HarborClient: nil}
}

// reconcilerWithHarbor returns a TenantReconciler with HarborClient set (Harbor mode).
func reconcilerWithHarbor() *TenantReconciler {
	return &TenantReconciler{HarborClient: harbor.NewClient("https://harbor.test", "admin", "pass")}
}

func TestAllConditionsTrue_RequiresPriorityClassReady(t *testing.T) {
	r := reconcilerWithoutHarbor()

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

func TestAllConditionsTrue_HarborProjectReady(t *testing.T) {
	t.Run("DOCR mode: does not require HarborProjectReady", func(t *testing.T) {
		r := reconcilerWithoutHarbor()
		conds := allFiveConditions() // 5 conditions without HarborProjectReady
		result := conditions.AllConditionsTrue(conds, r.requiredProvisioningConditions()...)
		if !result {
			t.Error("DOCR mode should not require HarborProjectReady")
		}
	})

	t.Run("Harbor mode: returns false when HarborProjectReady is missing", func(t *testing.T) {
		r := reconcilerWithHarbor()
		conds := allFiveConditions() // 5 conditions without HarborProjectReady
		result := conditions.AllConditionsTrue(conds, r.requiredProvisioningConditions()...)
		if result {
			t.Error("Harbor mode should require HarborProjectReady")
		}
	})

	t.Run("Harbor mode: returns false when HarborProjectReady is False", func(t *testing.T) {
		r := reconcilerWithHarbor()
		conds := allFiveConditions()
		conds = append(conds, metav1.Condition{
			Type:   conditions.TypeHarborProjectReady,
			Status: metav1.ConditionFalse,
		})
		result := conditions.AllConditionsTrue(conds, r.requiredProvisioningConditions()...)
		if result {
			t.Error("Harbor mode should fail when HarborProjectReady is False")
		}
	})

	t.Run("Harbor mode: returns true when all 6 conditions are True", func(t *testing.T) {
		r := reconcilerWithHarbor()
		conds := allSixConditions()
		result := conditions.AllConditionsTrue(conds, r.requiredProvisioningConditions()...)
		if !result {
			t.Error("Harbor mode should pass when all 6 conditions including HarborProjectReady are True")
		}
	})
}
