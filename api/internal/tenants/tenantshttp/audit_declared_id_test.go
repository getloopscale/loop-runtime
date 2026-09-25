// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenantshttp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// TestTenantsDeleteDeclaresTenantIDAsResourceID — B-059 FR-002.
//
// `extractResourceID` does not know the `tenants` collection, so the id in
// the URL never reached the audit row: tenant.deleted — the most
// destructive operation the product has — recorded an empty resource_id.
//
// The call below is refused (the caller is not that tenant's owner), which
// is the point: a refused delete must still be attributable to the tenant
// somebody tried to delete.
func TestTenantsDeleteDeclaresTenantIDAsResourceID(t *testing.T) {
	// Arrange.
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default(), func(string) bool { return false })
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-victim", nil)
	req = routestest.WithChiParam(req, "id", "tn-victim")
	req = routestest.WithAuditSlot(routestest.WithAuth(req, routestest.DefaultUserContext()))

	// Act.
	h.Delete(httptest.NewRecorder(), req)

	// Assert.
	if got := routestest.DeclaredResourceID(req); got != "tn-victim" {
		t.Fatalf("handler declared resource_id %q; want %q", got, "tn-victim")
	}
}
