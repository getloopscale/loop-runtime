// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cross-tenant isolation for the scale handler.
//
// Moved out of the parent's cross_tenant_test.go when the family was extracted
// (B-080). The isolation suite is genuinely cross-cutting, and splitting it is
// not free — but the routes fence refuses a PARENT → family edge even from a
// _test.go, because a parent that depends on a family can never be emptied.
//
// Same decision as the secrets family took, for the same reason.
package scale

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// Fixtures for this package's isolation tests, duplicated from the parent's
// suite rather than exported from it — same reason the secrets family gives:
// sharing them would re-create the parent→family edge the fence forbids.
const (
	xtTenantA  = "tn-xt-tenant-a"
	xtTenantB  = "tn-xt-tenant-b"
	xtUserB    = "github-xt-user-b"
	xtProjectA = "xt-project-a"
	xtEnvProd  = "production"
)

// tenantAContext returns a UserContext for the victim (tenant A).
func tenantAContext() *auth.UserContext {
	return &auth.UserContext{
		TenantID: xtTenantA,
		UserID:   "user-a",
	}
}

// tenantBContext returns a UserContext for the attacker (tenant B).
func tenantBContext() *auth.UserContext {
	return &auth.UserContext{
		UserID:   xtUserB,
		Email:    "xt-user-b@example.com",
		TenantID: xtTenantB,
		Role:     "owner",
	}
}

// rlsScaleProjectStore models PostgresProjectStore behavior where queries
// SCOPE by tenant_id. Tenant B looking up tenant A's project returns NIL.
type rlsScaleProjectStore struct {
	owningTenant string
	project      *ports.ProjectResult
}

func (s *rlsScaleProjectStore) GetByName(_ context.Context, callerTenant, name string) (*ports.ProjectResult, error) {
	if callerTenant != s.owningTenant {
		return nil, nil // RLS-scoped: invisible to other tenants
	}
	if s.project != nil && s.project.Name == name {
		return s.project, nil
	}
	return nil, nil
}

// TestCrossTenant_Scale_RLS_ReturnsNotFound is the RLS-faithful version. Tenant B
// tries to scale a project owned by tenant A; the store models tenant scoping
// and returns nil; the handler returns 404. The apply closure is a tripwire.
func TestCrossTenant_Scale_RLS_ReturnsNotFound(t *testing.T) {
	store := &rlsScaleProjectStore{
		owningTenant: xtTenantA,
		project:      &ports.ProjectResult{ID: "p1", Name: "tenant-a-api", TeamID: xtTenantA},
	}
	apply := func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		t.Fatal("apply must NOT be called on cross-tenant rejection")
		return false, nil
	}
	h := NewScaleHandler(store, apply, true)

	body := `{"environment":"production","app":"api","replicas":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/tenant-a-api/scale", bytes.NewBufferString(body))
	req = routestest.WithChiParam(req, "name", "tenant-a-api")
	req = routestest.WithAuth(req, tenantBContext())
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (project hidden across tenants), got %d body=%s", w.Code, w.Body.String())
	}
	// Response must not leak tenant A identity or the project shape.
	if bytes.Contains(w.Body.Bytes(), []byte(xtTenantA)) {
		t.Errorf("response leaks tenant A id: %s", w.Body.String())
	}
}

// Sanity: same handler MUST 200 OK for the owning tenant. Tightens the
// negative test above by proving the path works for the legitimate caller.
func TestCrossTenant_Scale_RLS_AllowsOwningTenant(t *testing.T) {
	store := &rlsScaleProjectStore{
		owningTenant: xtTenantA,
		project:      &ports.ProjectResult{ID: "p1", Name: "tenant-a-api", TeamID: xtTenantA},
	}
	applyCalled := false
	apply := func(_ context.Context, cfg ScaleConfigInput) (bool, error) {
		applyCalled = true
		if cfg.TenantID != xtTenantA {
			t.Errorf("apply called with wrong tenant: %s want %s", cfg.TenantID, xtTenantA)
		}
		return true, nil
	}
	h := NewScaleHandler(store, apply, true)

	body := `{"environment":"production","app":"api","replicas":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/tenant-a-api/scale", bytes.NewBufferString(body))
	req = routestest.WithChiParam(req, "name", "tenant-a-api")
	req = routestest.WithAuth(req, tenantAContext())
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for owning tenant, got %d body=%s", w.Code, w.Body.String())
	}
	if !applyCalled {
		t.Error("apply must be called for owning-tenant scale request")
	}
}
