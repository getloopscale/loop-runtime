// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtest"
)

// errRow is a Row whose Scan fails with err.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// A tenant with no row in `subscriptions` is the NORMAL state of a tenant that
// just signed up — not a platform fault. Measured 2026-08-18 (B-140): the
// missing row surfaced as `503 PLATFORM_DEGRADED` on POST /api/v1/projects
// until the store answered ("", nil), which ResolvePlanTier maps to Starter.
func TestPlanLimitsStoreTenantWithoutSubscriptionIsNotAnError(t *testing.T) {
	limits := NewPlanLimitsStore(&dbtest.FakeDBTX{})

	plan, err := limits.GetTenantPlan(context.Background(), "tn-new")

	if err != nil || plan != "" {
		t.Fatalf("GetTenantPlan = %q, %v; want the empty plan and no error", plan, err)
	}
}

// The boundary of the rule above: a REAL database error stays an error, or a
// database outage would read as "new tenant" and every limit as Starter's.
func TestPlanLimitsStoreDatabaseErrorStaysAnError(t *testing.T) {
	boom := errors.New("connection refused")
	limits := NewPlanLimitsStore(&dbtest.FakeDBTX{QueryRowResult: errRow{boom}})

	for name, lookup := range map[string]func() error{
		"tenant plan":  func() error { _, err := limits.GetTenantPlan(context.Background(), "tn-x"); return err },
		"account plan": func() error { _, err := limits.GetAccountPlanByTenant(context.Background(), "tn-x"); return err },
		"limits": func() error {
			_, _, err := limits.GetLimits(context.Background(), "pro", "projects")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := lookup(); !errors.Is(err, boom) {
				t.Fatalf("error = %v; want it to wrap the database error", err)
			}
		})
	}
}

// A missing plan_limits row is the (0, 0, nil) sentinel PlanLimiter denies on,
// not an error that would turn a missing configuration into an outage.
func TestPlanLimitsStoreMissingLimitRowIsTheDenySentinel(t *testing.T) {
	limits := NewPlanLimitsStore(&dbtest.FakeDBTX{})

	hard, soft, err := limits.GetLimits(context.Background(), "starter", "unknown_metric")

	if err != nil || hard != 0 || soft != 0 {
		t.Fatalf("GetLimits = (%d, %d, %v); want (0, 0, nil)", hard, soft, err)
	}
}
