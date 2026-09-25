// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cross-tenant isolation for the build handler.
//
// Moved out of the parent's cross_tenant_test.go when the family was extracted
// (B-080, slice-37). Same decision the secrets and scale families took: the
// routes fence refuses a PARENT → family edge even from a _test.go, because a
// parent that depends on a family can never be emptied.
package buildshttp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// Fixtures for this package's isolation tests, duplicated from the parent's
// suite rather than exported from it — DRY governs knowledge, not lines, and
// sharing them would re-create the forbidden edge.
const (
	xtTenantA = "tn-xt-tenant-a"
	xtTenantB = "tn-xt-tenant-b"
	xtUserB   = "github-xt-user-b"
	xtBuildA  = "bld-xt-a"
)

// tenantBContext returns a UserContext for the attacker (tenant B).
func tenantBContext() *auth.UserContext {
	return &auth.UserContext{
		UserID:   xtUserB,
		Email:    "xt-user-b@example.com",
		TenantID: xtTenantB,
		Role:     "owner",
	}
}

// TestCrossTenant_Build_Get_ReturnsNotFound_WhenTenantMismatch verifies
// that BuildHandler.Get returns 404 (not 403) when an authenticated tenant
// requests a build owned by another tenant. SEC-4 invariant: 404 hides
// existence; 403 would leak the build ID space.
func TestCrossTenant_Build_Get_ReturnsNotFound_WhenTenantMismatch(t *testing.T) {
	// Arrange: fake orchestrator reports the build belongs to tenant A.
	orch := &fakeBuildOrch{
		tenantID: xtTenantA, // build IS tenant A's
		// statusResult would be returned IF tenant check passed
		statusResult: &BuildStatusResponse{Status: "RUNNING"},
	}
	h := NewBuildHandler(orch, slog.Default())

	// Act: tenant B requests it.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+xtBuildA, nil)
	req = routestest.WithChiParam(req, "id", xtBuildA)
	req = routestest.WithAuth(req, tenantBContext())
	w := httptest.NewRecorder()
	h.Get(w, req)

	// Assert: 404 (hidden), not 403 (leaks existence), not 200 (data leak).
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (build hidden across tenants), got %d body=%s",
			w.Code, w.Body.String())
	}
	// Response body must NOT include any tenant A data.
	if bytes.Contains(w.Body.Bytes(), []byte(xtTenantA)) {
		t.Errorf("response leaks tenant A id: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("RUNNING")) {
		t.Errorf("response leaks tenant A build status: %s", w.Body.String())
	}
}

// TestCrossTenant_Build_Cancel_ReturnsNotFound_WhenTenantMismatch verifies
// that BuildHandler.Cancel rejects cross-tenant cancel attempts with 404,
// and that CancelBuild is NEVER called on the orchestrator.
func TestCrossTenant_Build_Cancel_ReturnsNotFound_WhenTenantMismatch(t *testing.T) {
	// Arrange: fake reports build belongs to tenant A. cancelErr is non-nil
	// so any accidental call surfaces as 500 (would fail the test below).
	orch := &fakeBuildOrch{tenantID: xtTenantA}
	h := NewBuildHandler(orch, slog.Default())

	// Act
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/builds/"+xtBuildA, nil)
	req = routestest.WithChiParam(req, "id", xtBuildA)
	req = routestest.WithAuth(req, tenantBContext())
	w := httptest.NewRecorder()
	h.Cancel(w, req)

	// Assert
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (cancel rejected cross-tenant), got %d body=%s",
			w.Code, w.Body.String())
	}
}

// TestCrossTenant_Build_Get_Unauthenticated_Returns401 ensures the
// unauthenticated path is closed (defense in depth — middleware would
// normally block this, but the handler must reject too).
func TestCrossTenant_Build_Get_Unauthenticated_Returns401(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{tenantID: xtTenantA}, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+xtBuildA, nil)
	req = routestest.WithChiParam(req, "id", xtBuildA)
	// No withAuth — adversarial: bypass middleware accidentally.
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestCrossTenant_Build_Cancel_Unauthenticated_Returns401 same as above for cancel.
func TestCrossTenant_Build_Cancel_Unauthenticated_Returns401(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{tenantID: xtTenantA}, slog.Default())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/builds/"+xtBuildA, nil)
	req = routestest.WithChiParam(req, "id", xtBuildA)
	w := httptest.NewRecorder()
	h.Cancel(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}
