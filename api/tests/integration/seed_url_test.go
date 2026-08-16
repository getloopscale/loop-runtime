// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import "os"

// seedDBURL is the connection a FIXTURE should use, which is not the same as
// the connection a REQUEST should use.
//
// Since migration 000062 the tenant tables are fail-closed, and 000064 removed
// the owner's standing `*_owner_maintenance` bypass. A fixture that seeds two
// tenants to prove a cross-tenant property is doing cross-tenant work by
// definition — the same job the background loops do — so it connects the same
// way they do: as `loopzero_worker`, whose reach is the NAMED
// `*_worker_cross_tenant` policies (migration 000062).
//
// The alternative would be to restore a broad standing privilege for the owner
// so fixtures keep working. That is the bypass this whole line of work removed,
// and re-adding it for tests would make the suite green over a posture
// production does not have — which is the exact failure the 2026-08-14 audit
// found (a tenant-isolation test asserting isolation over a connection that
// structurally could not be isolated).
//
// Falls back to DATABASE_URL when the worker DSN is unset, so a developer
// machine without the role split still runs the suite.
func seedDBURL() string {
	if url := os.Getenv("LOOPZERO_WORKER_DATABASE_URL"); url != "" {
		return url
	}
	return dbURL()
}
