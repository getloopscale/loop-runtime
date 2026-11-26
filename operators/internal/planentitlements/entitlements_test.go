// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package planentitlements

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

func TestFor_StarterGetsTheBaseTier(t *testing.T) {
	e := For(loopruntimev1.PlanStarter)
	if e.PriorityClassName != "loopruntime-tenant-starter" || e.PriorityValue != 100 || e.Preemption != corev1.PreemptNever {
		t.Fatalf("starter priority = %+v", e)
	}
	if e.ManagesSecrets || e.ManagesNetworking {
		t.Fatalf("starter entitlements = %+v", e)
	}
}

// The api writes the canonical tiers (pkg/domain.PlanTier): a pro tenant must
// get the paid mid tier, not fall through to starter.
func TestFor_ProGetsThePaidMidTier(t *testing.T) {
	e := For(loopruntimev1.PlanPro)
	if e.PriorityClassName != "loopruntime-tenant-growth" || e.PriorityValue != 500 || e.Preemption != corev1.PreemptLowerPriority {
		t.Fatalf("pro priority = %+v", e)
	}
	if !e.ManagesSecrets || e.ManagesNetworking {
		t.Fatalf("pro entitlements = %+v", e)
	}
}

func TestFor_TeamGetsThePaidMidTier(t *testing.T) {
	if got, want := For(loopruntimev1.PlanTeam), For(loopruntimev1.PlanPro); got != want {
		t.Fatalf("team = %+v, want the pro entitlements %+v", got, want)
	}
}

// "growth" is the pre-2026-05-12 name for pro; CRs stored with it keep their tier.
func TestFor_LegacyGrowthMeansPro(t *testing.T) {
	if got, want := For(loopruntimev1.PlanGrowth), For(loopruntimev1.PlanPro); got != want {
		t.Fatalf("growth = %+v, want the pro entitlements %+v", got, want)
	}
}

func TestFor_EnterpriseGetsEverything(t *testing.T) {
	e := For(loopruntimev1.PlanEnterprise)
	if e.PriorityClassName != "loopruntime-tenant-enterprise" || e.PriorityValue != 1000 {
		t.Fatalf("enterprise priority = %+v", e)
	}
	if !e.ManagesSecrets || !e.ManagesNetworking {
		t.Fatalf("enterprise entitlements = %+v", e)
	}
}

// An unrecognised value never grants more than starter.
func TestFor_UnknownPlanFallsBackToStarter(t *testing.T) {
	if got, want := For(loopruntimev1.PlanTier("platinum")), For(loopruntimev1.PlanStarter); got != want {
		t.Fatalf("unknown plan = %+v, want starter %+v", got, want)
	}
}
