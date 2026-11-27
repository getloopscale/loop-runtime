// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"testing"

	loopruntimedb "github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	policy "github.com/getloopscale/loop-runtime/api/internal/middleware/policy"
)

// The idempotency key is chosen by the CLIENT — it arrives in the
// `Idempotency-Key` header as arbitrary text. Until migration 000066 the
// primary key was `(key)` alone, so two tenants that picked the same string
// (`deploy-1`, today's date, a branch name) collided on one row.
//
// What that produced, and what it did NOT:
//
//   - It DID deny service. The record is written AFTER the response, so the
//     second tenant's request did its work and then died on a unique violation.
//     Worse, the namespace is occupiable on purpose: fill it with likely strings
//     and another tenant cannot deploy, without ever touching their data.
//   - It did NOT leak. `GetIdempotencyRecord` has always matched
//     `key = $1 AND tenant_id = $2`, so nobody ever received a response stored
//     by someone else.
//
// Saying which of the two it was matters more than the scare. This test pins
// both halves: the same key works for two tenants, and each one reads only its
// own record.
//
// @see internal/db/migrations/000066_idempotency_key_per_tenant.up.sql
func TestIdempotencyKey_SameKeyTwoTenants(t *testing.T) {
	ctx := context.Background()
	pool := poolFor(t, dsnOrSkip(t, "LOOP_RUNTIME_WORKER_DATABASE_URL"))

	const (
		key     = "deploy-1" // exactly the shape a human picks
		tenantA = "tn-idem-a"
		tenantB = "tn-idem-b"
	)

	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE key = $1`, key)
	}
	cleanup()
	t.Cleanup(cleanup)

	// The cross-tenant connection is what a test fixture spanning two tenants
	// needs — the same rule production follows for workspace-spanning work.
	st := &store.PostgresIdempotencyStore{DB: &loopruntimedb.PgxAdapter{Pool: pool}}

	for _, tenant := range []string{tenantA, tenantB} {
		rec := &policy.IdempotencyRecord{
			Key:            key,
			Endpoint:       "POST /api/v1/builds",
			TenantID:       tenant,
			RequestHash:    "hash-" + tenant,
			ResponseStatus: 202,
			ResponseBody:   []byte(`{"ok":true}`),
		}
		if err := st.Store(ctx, rec); err != nil {
			t.Fatalf("tenant %s could not store the key %q: %v — "+
				"a client-chosen key must not be occupiable by another tenant", tenant, key, err)
		}
	}

	// And the read stays each tenant's own. This half was never broken; it is
	// asserted so a future widening of the key cannot quietly break it either.
	for _, tc := range []struct{ tenant, wantHash string }{
		{tenantA, "hash-" + tenantA},
		{tenantB, "hash-" + tenantB},
	} {
		got, err := st.Get(ctx, key, tc.tenant)
		if err != nil {
			t.Fatalf("get for %s: %v", tc.tenant, err)
		}
		if got == nil {
			t.Fatalf("%s stored the key and cannot read it back", tc.tenant)
		}
		if got.RequestHash != tc.wantHash {
			t.Errorf("%s read the record of another tenant: request hash = %q, want %q",
				tc.tenant, got.RequestHash, tc.wantHash)
		}
	}
}
