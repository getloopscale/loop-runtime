// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Boot guard — db.AssertRequestPoolIsolated.
//
// The guard is the piece that keeps the 2026-08-14 exposure from returning by
// configuration drift. The policies can be perfect and still be inert if the
// process connects with the wrong role, and that mistake produces no runtime
// signal at all. These tests pin both directions: the correct role boots, and
// each escape is refused.
package integration

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// TestRLSGuard_AcceptsRequestRole — loopruntime_api is non-owner and non-bypassing,
// so the guard must let the process start.
func TestRLSGuard_AcceptsRequestRole(t *testing.T) {
	ctx, pool := auditPool(t, "LOOPRUNTIME_API_DATABASE_URL", "request path (loopruntime_api)")

	if err := db.AssertRequestPoolIsolated(ctx, pool); err != nil {
		t.Errorf("the guard rejected the intended request role: %v\n"+
			"If this fails, either loopruntime_api regained a bypass or it became the table owner.", err)
	}
}

// TestRLSGuard_RefusesOwner — the owner reads across tenants (it holds DDL and
// carries the maintenance policy), so it must never serve requests. Before
// 000062 this was exactly how the engine ran.
func TestRLSGuard_RefusesOwner(t *testing.T) {
	ctx, pool := auditPool(t, "DATABASE_URL", "owner")

	posture, err := db.MeasureRLSPosture(ctx, pool, "builds")
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if !posture.IsOwner {
		// Same class as the audit suite: the assertion needs a privileged role the
		// default environment does not provide, so the control goes unmeasured
		// rather than green-by-accident.
		// Tracked: https://github.com/getloopscale/loop-runtime/issues/289
		t.Skipf("DATABASE_URL is not the table owner (%s) — nothing to assert here", posture)
	}

	err = db.AssertRequestPoolIsolated(ctx, pool)
	if err == nil {
		t.Fatalf("the guard accepted the OWNER as a request pool (%s).\n"+
			"This is the exact configuration measured on 2026-08-14, in which every "+
			"isolation policy was inert on every HTTP request.", posture)
	}
	if !strings.Contains(err.Error(), "refusing to serve requests") {
		t.Errorf("guard rejected the owner but with an unexpected message: %v", err)
	}
	t.Logf("guard correctly refused the owner: %s", posture)
}

// TestRLSGuard_RefusesBypassRole — BYPASSRLS is the other escape, and it is
// independent of ownership: a non-owner role with the attribute still reads
// every tenant. Uses the worker DSN only to reach a role it can flip, and
// restores the attribute afterwards.
func TestRLSGuard_RefusesBypassRole(t *testing.T) {
	// Granting BYPASSRLS requires SUPERUSER — CREATEROLE is not enough — so
	// this one case needs a superuser DSN. Provided in CI; absent locally the
	// test skips rather than pretending to have covered the attribute.
	octx, super := auditPool(t, "LOOPRUNTIME_RLS_AUDIT_SUPERUSER_URL", "superuser")

	if _, err := super.Exec(octx, `ALTER ROLE loopruntime_api BYPASSRLS`); err != nil {
		// Tracked: https://github.com/getloopscale/loop-runtime/issues/289
		t.Skipf("cannot grant BYPASSRLS on loopruntime_api (needs SUPERUSER): %v", err)
	}
	t.Cleanup(func() {
		if _, err := super.Exec(octx, `ALTER ROLE loopruntime_api NOBYPASSRLS`); err != nil {
			t.Fatalf("could not restore NOBYPASSRLS on loopruntime_api: %v — "+
				"the test database is left with a request role that bypasses RLS", err)
		}
	})

	// A fresh pool: rolbypassrls is resolved per session, so an existing
	// connection would still report the old attribute.
	ctx, pool := auditPool(t, "LOOPRUNTIME_API_DATABASE_URL", "request path with BYPASSRLS")

	err := db.AssertRequestPoolIsolated(ctx, pool)
	if err == nil {
		t.Error("the guard accepted a BYPASSRLS role as a request pool — " +
			"policies are never evaluated for such a role")
	} else if !strings.Contains(err.Error(), "BYPASSRLS") {
		t.Errorf("guard rejected the role but did not name BYPASSRLS as the cause: %v", err)
	}
}
