// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package plantier

import (
	"context"
	"errors"
	"testing"

	pkgdomain "github.com/getloopscale/loop-runtime/pkg/domain"
)

type fakeStore struct {
	plan string
	err  error
}

func (f fakeStore) GetTenantPlan(context.Context, string) (string, error) { return f.plan, f.err }

func TestResolve_NoStoreIsStarter(t *testing.T) {
	tier, terr := Resolve(context.Background(), nil, "tn-1")
	if terr != nil || tier != pkgdomain.PlanTierStarter {
		t.Fatalf("Resolve(nil store) = (%s, %v), want (starter, nil)", tier, terr)
	}
}

func TestResolve_NoSubscriptionIsStarter(t *testing.T) {
	tier, terr := Resolve(context.Background(), fakeStore{}, "tn-1")
	if terr != nil || tier != pkgdomain.PlanTierStarter {
		t.Fatalf("got (%s, %v), want (starter, nil)", tier, terr)
	}
}

func TestResolve_ReturnsTheStoredTier(t *testing.T) {
	tier, terr := Resolve(context.Background(), fakeStore{plan: "team"}, "tn-1")
	if terr != nil || tier != pkgdomain.PlanTierTeam {
		t.Fatalf("got (%s, %v), want (team, nil)", tier, terr)
	}
}

func TestResolve_StoreFailureIsDegradedNotStarter(t *testing.T) {
	tier, terr := Resolve(context.Background(), fakeStore{err: errors.New("connection refused")}, "tn-1")
	if terr == nil || terr.Code != "PLATFORM_DEGRADED" {
		t.Fatalf("want PLATFORM_DEGRADED, got %v", terr)
	}
	if tier == pkgdomain.PlanTierStarter {
		t.Error("a failed read must not produce a Starter tier a caller could act on")
	}
}

func TestResolve_UnknownPlanValueIsDegraded(t *testing.T) {
	_, terr := Resolve(context.Background(), fakeStore{plan: "platinum"}, "tn-1")
	if terr == nil || terr.Code != "PLATFORM_DEGRADED" {
		t.Fatalf("want PLATFORM_DEGRADED for an unknown plan value, got %v", terr)
	}
}
