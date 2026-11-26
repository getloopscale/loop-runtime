// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package planentitlements is the one place that says what a subscription
// plan buys on the runtime cluster: scheduling priority, registry quota and
// the extra RBAC an environment's deployer gets. The tenant and environment
// controllers read it instead of each switching on the plan.
package planentitlements

import (
	corev1 "k8s.io/api/core/v1"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

// Entitlements is what one plan tier grants.
type Entitlements struct {
	PriorityClassName string
	PriorityValue     int32
	Preemption        corev1.PreemptionPolicy
	// ManagesSecrets lets the environment deployer manage Secrets.
	ManagesSecrets bool
	// ManagesNetworking lets the environment deployer manage Ingresses and
	// NetworkPolicies.
	ManagesNetworking bool
}

var (
	starter = Entitlements{
		PriorityClassName: constants.PriorityClassStarter,
		PriorityValue:     constants.PriorityValueStarter,
		Preemption:        corev1.PreemptNever,
	}
	// paid is the mid tier. Its PriorityClass keeps the "growth" name that
	// clusters already carry from before pro and team existed.
	paid = Entitlements{
		PriorityClassName: constants.PriorityClassGrowth,
		PriorityValue:     constants.PriorityValueGrowth,
		Preemption:        corev1.PreemptLowerPriority,
		ManagesSecrets:    true,
	}
	enterprise = Entitlements{
		PriorityClassName: constants.PriorityClassEnterprise,
		PriorityValue:     constants.PriorityValueEnterprise,
		Preemption:        corev1.PreemptLowerPriority,
		ManagesSecrets:    true,
		ManagesNetworking: true,
	}
)

// For returns the entitlements of plan. Pro and team share the paid tier,
// as does the legacy "growth" name for pro; any value this table does not
// know gets starter, so an unexpected plan never grants more.
func For(plan loopruntimev1.PlanTier) Entitlements {
	switch plan {
	case loopruntimev1.PlanPro, loopruntimev1.PlanTeam, loopruntimev1.PlanGrowth:
		return paid
	case loopruntimev1.PlanEnterprise:
		return enterprise
	default:
		return starter
	}
}
