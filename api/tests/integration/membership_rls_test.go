// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

// Authentication resolves which teams a user belongs to BEFORE any team is
// selected, and a user may belong to several: the lookup spans tenants by
// definition. tenant_members is under fail-closed Row Level Security, so over
// the request role it found no team and every authenticated call answered
// 403 NO_TENANT. These tests pin which connection can answer it — the
// composition root builds the membership store on the cross-tenant pool.
//
//	DATABASE_URL=<loopruntime_api> LOOPRUNTIME_WORKER_DATABASE_URL=<loopruntime_worker> \
//	  go test ./tests/integration/ -run MembershipStore -v
const membershipOwner = "rls-member-uid"

func membershipSeed(t *testing.T) {
	t.Helper()
	_ = testPool(t) // skips when PostgreSQL is unreachable
	seed, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open seed DB: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	projectCapSeed(t, seed, membershipOwner, "acct-rls-member", "tn-rls-member-a", "tn-rls-member-b", "starter")
	t.Cleanup(func() { projectCapCleanup(t, seed, membershipOwner) })
	for _, tn := range []string{"tn-rls-member-a", "tn-rls-member-b"} {
		if _, err := seed.Exec(`INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, 'owner')`,
			tn, membershipOwner); err != nil {
			t.Fatalf("seed membership in %s: %v", tn, err)
		}
	}
}

func TestMembershipStoreOnTheWorkerRoleFindsEveryTeamOfTheUser(t *testing.T) {
	membershipSeed(t)
	members := &store.PostgresMembershipStore{DB: &db.PgxAdapter{Pool: workerPool(t)}}

	got, err := members.GetMemberships(context.Background(), membershipOwner)

	if err != nil || len(got) != 2 {
		t.Fatalf("memberships over the worker role = %v, %v; want both teams", got, err)
	}
}

// The failure the cross-tenant wiring prevents: before a team is selected the
// request role has no scope, and RLS hides every membership row.
func TestMembershipStoreOnTheRequestRoleFindsNoTeam(t *testing.T) {
	requireIsolatedRequestRole(t)
	membershipSeed(t)
	members := &store.PostgresMembershipStore{DB: &db.PgxAdapter{Pool: poolFor(t, dbURL())}}

	got, err := members.GetMemberships(context.Background(), membershipOwner)

	if err != nil || len(got) != 0 {
		t.Fatalf("memberships over the request role = %v, %v; want none (hidden by RLS)", got, err)
	}
}
