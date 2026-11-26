// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// Eviction deletes across nine tables. If a late step fails, the earlier ones
// must not stay committed: a half-deleted tenant (projects gone, tenant row
// still there) is a state no other code path expects. Here the tenants DELETE
// fails on a custom domain the cascade does not cover (FK RESTRICT), and the
// project deleted at step 5 must still be present afterwards.
func TestDeleteTenantsFKOrdered_AFailedStepLeavesNothingDeleted(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-evict-atomic"
	seedGCTenant(t, ctx, pool, tenantID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM custom_domains WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE tenant_id=$1`, tenantID)
	})
	for _, q := range []string{
		`INSERT INTO projects (id, tenant_id, name, created_by) VALUES ('p-evict-atomic', $1, 'web', 'u-' || $1)`,
		`INSERT INTO custom_domains (id, tenant_id, project_name, app_name, environment, domain, verification_token)
		 VALUES ('cd-evict-atomic', $1, 'web', 'api', 'production', 'evict-atomic.example.com', 'tok')`,
	} {
		if _, err := pool.Exec(ctx, q, tenantID); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	s := NewTenantStoreSqlc(&db.PgxAdapter{Pool: pool})
	if _, err := s.DeleteTenantsFKOrdered(ctx, []string{tenantID}); err == nil {
		t.Fatal("the tenants step must fail while a custom domain still references the tenant")
	}

	var projects int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM projects WHERE tenant_id=$1`, tenantID).Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if projects != 1 {
		t.Fatalf("the failed eviction left %d projects; the steps before the failure must roll back", projects)
	}
}
