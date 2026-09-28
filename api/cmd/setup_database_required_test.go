// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db/dbtest"
)

// A missing `DATABASE_URL` is a CONFIGURATION error, not degradation — decided
// in MEET-ASYNC-AMENDMENT-2026-08-17-no-database-is-a-config-error.
//
// Three measurements back the decision: the degradation never ran (it panicked
// three functions further on, `B-104`); its reach is 27 of the router's 36
// surfaces; and its only recorded occurrence in four months was a bootstrap
// broken in silence (B35, 2026-04-28, commented in the deployment itself).

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSetupDatabase_MissingURLIsATypedBootError(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	deps, err := setupDatabase(config.Config{}, quietLogger())

	if err == nil {
		t.Fatal("a missing DATABASE_URL must fail the boot: no amount of waiting produces the value, and 27 of the 36 surfaces depend on it")
	}
	if !errors.Is(err, ErrDatabaseURLNotSet) {
		t.Fatalf("the error must be recognisable by whoever handles the boot, not a string; got %T: %v", err, err)
	}
	if deps != nil {
		t.Fatalf("nothing was built; returning non-nil deps alongside an error invites the caller to use them: %+v", deps)
	}
}

// The message is the only diagnosis the operator gets from the first
// `kubectl logs`, and the MEET's devil's advocate accepted the `CrashLoopBackOff`
// precisely because it names the variable.
func TestSetupDatabase_MissingURLErrorNamesTheVariable(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	_, err := setupDatabase(config.Config{}, quietLogger())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("the message must name the missing variable; got %q", err.Error())
	}
}

// The opposite boundary: with the variable present, `setupDatabase` must NOT
// refuse for this reason. Without this assertion, hardening the boot could turn
// into a rule no environment satisfies — and the value here is deliberately
// invalid, so the test needs no Postgres to prove what it has to prove.
func TestSetupDatabase_PresentURLIsNotRejectedAsMissing(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")

	_, err := setupDatabase(config.Config{}, quietLogger())
	if errors.Is(err, ErrDatabaseURLNotSet) {
		t.Fatalf("a present URL is never 'missing'; got %v", err)
	}
}

// Plan enforcement (project cap, replica cap, plan limits) reads the plan
// through the cross-tenant connection main hands it — never through the request
// pool, where fail-closed RLS hides every subscription and each tenant would be
// capped as Starter. The store must sit on exactly the handle it was given.
func TestWirePlanEnforcementReadsThroughTheCrossTenantConnection(t *testing.T) {
	crossTenant := &dbtest.FakeDBTX{}
	deps := &DatabaseDeps{}

	wirePlanEnforcement(deps, crossTenant, quietLogger())

	if deps.PlanLimiter == nil || deps.PlanLimits == nil {
		t.Fatalf("plan enforcement not wired: limiter=%v store=%v", deps.PlanLimiter, deps.PlanLimits)
	}
	if deps.PlanLimits.DB != crossTenant {
		t.Fatalf("plan-limits store reads through %T, want the cross-tenant connection", deps.PlanLimits.DB)
	}
}
