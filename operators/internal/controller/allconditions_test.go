package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/loopzero/loopzero/operators/internal/conditions"
)

// allSevenConditions returns conditions for the original 7 required types, all True.
func allSevenConditions() []metav1.Condition {
	types := []string{
		conditions.TypeGitRepoReady,
		conditions.TypeNamespacesReady,
		conditions.TypeRBACReady,
		conditions.TypeNetworkPolicyReady,
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

// allEightConditions returns all 8 required conditions (including PriorityClassReady), all True.
func allEightConditions() []metav1.Condition {
	conds := allSevenConditions()
	conds = append(conds, metav1.Condition{
		Type:   conditions.TypePriorityClassReady,
		Status: metav1.ConditionTrue,
	})
	return conds
}

func TestAllConditionsTrue_RequiresPriorityClassReady(t *testing.T) {
	t.Run("returns false when PriorityClassReady is missing", func(t *testing.T) {
		// Arrange: all conditions True except PriorityClassReady is absent
		conds := allSevenConditions()

		// Act
		result := allConditionsTrue(conds)

		// Assert
		if result {
			t.Error("allConditionsTrue should return false when PriorityClassReady is missing")
		}
	})

	t.Run("returns false when PriorityClassReady is False", func(t *testing.T) {
		// Arrange: all 7 True + PriorityClassReady False
		conds := allSevenConditions()
		conds = append(conds, metav1.Condition{
			Type:   conditions.TypePriorityClassReady,
			Status: metav1.ConditionFalse,
		})

		// Act
		result := allConditionsTrue(conds)

		// Assert
		if result {
			t.Error("allConditionsTrue should return false when PriorityClassReady is False")
		}
	})

	t.Run("returns true when all 8 conditions including PriorityClassReady are True", func(t *testing.T) {
		// Arrange
		conds := allEightConditions()

		// Act
		result := allConditionsTrue(conds)

		// Assert
		if !result {
			t.Error("allConditionsTrue should return true when all 8 conditions are True")
		}
	})
}
