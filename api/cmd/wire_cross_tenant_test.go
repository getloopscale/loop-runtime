// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
)

// Resolving a user's teams spans tenants and runs before any team is selected,
// so it cannot run on the RLS-bound request pool: there it finds no team and
// every authenticated call is refused (tests/integration/membership_rls_test.go).
func TestTheMembershipLookupReadsThroughTheCrossTenantPool(t *testing.T) {
	crossTenant := &db.PgxAdapter{}
	deps := &DatabaseDeps{}

	wireCrossTenantLookups(deps, crossTenant)

	members, ok := deps.MemberStore.(*store.PostgresMembershipStore)
	if !ok || members.DB != crossTenant {
		t.Fatalf("MemberStore = %#v; want a membership store on the cross-tenant pool", deps.MemberStore)
	}
}

// crossTenantMarker is a handle the admission querier can be traced to: it runs
// no query, and the error naming its type proves the query was sent to it.
type crossTenantMarker struct{ nonPgxDBTX }

// The admission gate caps concurrent builds per CELL, counting every tenant on
// it; over the request pool, scoped to the caller, it would count the caller's
// builds alone (tests/integration/build_admission_rls_test.go).
func TestTheBuildAdmissionCountReadsThroughTheCrossTenantPool(t *testing.T) {
	querier := newAdmissionQuerier(crossTenantMarker{})

	_, err := querier.ActiveBuilds(context.Background(), "cell-0")

	if err == nil || !strings.Contains(err.Error(), "crossTenantMarker") {
		t.Fatalf("admission count went to %v; want the cross-tenant pool", err)
	}
}

// GitHub installations and repository links are global objects the webhook and
// the API's owner checks look up across tenants; over the request pool another
// tenant's installation is hidden and its owner check passes
// (tests/integration/repository_store_rls_test.go).
func TestTheRepositoryStoreReadsThroughTheCrossTenantPool(t *testing.T) {
	repos := newRepositoryStore(crossTenantMarker{})

	_, err := repos.GetInstallation(context.Background(), 1)

	if err == nil || !strings.Contains(err.Error(), "crossTenantMarker") {
		t.Fatalf("installation lookup went to %v; want the cross-tenant pool", err)
	}
}
