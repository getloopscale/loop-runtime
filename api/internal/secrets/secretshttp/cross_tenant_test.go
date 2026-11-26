// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Cross-tenant isolation for the secret handler.
//
// These came out of the parent package's cross_tenant_test.go when the family
// was extracted (B-080). The isolation suite is genuinely cross-cutting — it
// covers builds, secrets and scale — and splitting it was not free. What decided
// it was the fence: the routes fence refused a PARENT → family edge even from a
// _test.go, because a parent that depends on a family can never be emptied, and
// emptying it was the whole point of the decomposition. feature-fence keeps the
// same rule between features: one HTTP package never imports another.
//
// So each fragment lives with the family it guards. Same principle that moved
// the coverage tests: a test belongs with the code it exercises.
package secretshttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// Fixtures for this package's isolation tests.
//
// Duplicated from the parent's suite rather than exported from it, and that is
// deliberate: DRY governs knowledge, not lines. These are test constants — an
// attacker tenant, a victim tenant, a project name — and a package that owns its
// own fixtures can be read, moved and deleted on its own. Sharing them would
// re-create the parent→family edge the fence forbids, in the one direction that
// looks harmless.
const (
	xtTenantA  = "tn-xt-tenant-a"
	xtTenantB  = "tn-xt-tenant-b"
	xtUserB    = "github-xt-user-b"
	xtSecretA  = "XT_SECRET_A"
	xtProjectA = "xt-project-a"
	xtEnvProd  = "production"
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

// crossTenantSecretStore is a fake SecretStore that records which tenant_id
// the handler passes to it. The adversarial test then asserts that the
// recorded tenant is the CALLER's tenant (B), NEVER tenant A — even when
// the URL/body references tenant A's project.
type crossTenantSecretStore struct {
	// recorded values from the last call
	gotTenant  string
	gotProject string
	gotEnv     string
	gotApp     string
	gotName    string
	// stubbed responses
	listSecrets []ports.SecretMetadata
	setCreated  bool
	delDeleted  bool
	err         error
}

func (f *crossTenantSecretStore) ListNames(_ context.Context, tenantID, project, env string) ([]ports.SecretMetadata, error) {
	f.gotTenant, f.gotProject, f.gotEnv = tenantID, project, env
	return f.listSecrets, f.err
}

func (f *crossTenantSecretStore) Set(_ context.Context, tenantID, project, env, app, name string, _ []byte) (bool, error) {
	f.gotTenant, f.gotProject, f.gotEnv, f.gotApp, f.gotName = tenantID, project, env, app, name
	return f.setCreated, f.err
}

func (f *crossTenantSecretStore) Delete(_ context.Context, tenantID, project, env, app, name string) (bool, error) {
	f.gotTenant, f.gotProject, f.gotEnv, f.gotApp, f.gotName = tenantID, project, env, app, name
	return f.delDeleted, f.err
}

// TestCrossTenant_Secret_List_PassesCallerTenantOnly verifies that listing
// secrets is scoped to the caller's tenant. The handler ignores any query
// parameter that could imply a different tenant.
func TestCrossTenant_Secret_List_PassesCallerTenantOnly(t *testing.T) {
	store := &crossTenantSecretStore{listSecrets: nil}
	h := NewSecretHandler(store, &lockCountingFake{})

	// Act: tenant B lists secrets, querying with tenant A's project name.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/secrets?env="+xtEnvProd+"&project="+xtProjectA, nil)
	req = routestest.WithAuth(req, tenantBContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	// Assert: handler passed CALLER's tenant (B), even though URL has A's project.
	if store.gotTenant != xtTenantB {
		t.Fatalf("handler passed wrong tenant to store: got %q, want %q (caller's)",
			store.gotTenant, xtTenantB)
	}
	// Project from URL is passed through (RLS at DB ensures isolation).
	if store.gotProject != xtProjectA {
		t.Errorf("handler dropped project param: got %q", store.gotProject)
	}
	// Status code is OK (empty list is success).
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestCrossTenant_Secret_Set_PassesCallerTenantOnly verifies that POST
// /api/v1/secrets writes under the caller's tenant, not under any
// tenant-id implied by the request body or URL query.
func TestCrossTenant_Secret_Set_PassesCallerTenantOnly(t *testing.T) {
	store := &crossTenantSecretStore{setCreated: true}
	h := NewSecretHandler(store, &lockCountingFake{})

	// Act: tenant B writes a secret, with query project = tenant A's project.
	body := `{"name":"` + xtSecretA + `","value":"adversarial","environment":"` +
		xtEnvProd + `","app":"any-app"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/secrets?project="+xtProjectA, bytes.NewBufferString(body))
	req = routestest.WithAuth(req, tenantBContext())
	w := httptest.NewRecorder()
	h.Set(w, req)

	// Assert
	if store.gotTenant != xtTenantB {
		t.Fatalf("write went under wrong tenant: got %q, want %q",
			store.gotTenant, xtTenantB)
	}
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestCrossTenant_Secret_Delete_PassesCallerTenantOnly verifies the
// adversarial path for DELETE: cross-tenant delete attempts target the
// CALLER's tenant only.
func TestCrossTenant_Secret_Delete_PassesCallerTenantOnly(t *testing.T) {
	store := &crossTenantSecretStore{delDeleted: false} // not found under tenant B
	h := NewSecretHandler(store, &lockCountingFake{})

	// Act
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/secrets/"+xtSecretA+"?environment="+xtEnvProd+
			"&app=any&project="+xtProjectA, nil)
	req = routestest.WithChiParam(req, "name", xtSecretA)
	req = routestest.WithAuth(req, tenantBContext())
	w := httptest.NewRecorder()
	h.DeleteSecret(w, req)

	// Assert
	if store.gotTenant != xtTenantB {
		t.Fatalf("delete went under wrong tenant: got %q, want %q",
			store.gotTenant, xtTenantB)
	}
	// Secret doesn't exist under B → 404.
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 (no such secret in caller's tenant), got %d body=%s",
			w.Code, w.Body.String())
	}
}

// TestCrossTenant_Secret_List_Unauthenticated_Returns401 same for secret list.
func TestCrossTenant_Secret_List_Unauthenticated_Returns401(t *testing.T) {
	h := NewSecretHandler(&crossTenantSecretStore{}, &lockCountingFake{})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/secrets?env="+xtEnvProd, nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestCrossTenant_Secret_Set_IgnoresTenantInBody verifies that even if the
// request body contains a `tenant_id` field (which the schema does not
// declare but a malicious client could send), the handler ignores it.
// Currently secretSetRequest does not parse a tenant field — this test
// guards against future schema drift that might add one.
func TestCrossTenant_Secret_Set_IgnoresTenantInBody(t *testing.T) {
	store := &crossTenantSecretStore{setCreated: true}
	h := NewSecretHandler(store, &lockCountingFake{})

	// Adversarial body: includes a tenant_id field that should be IGNORED.
	body := `{"name":"VAR","value":"v","environment":"prod","app":"a","tenant_id":"` + xtTenantA + `"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/secrets", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, tenantBContext())
	w := httptest.NewRecorder()
	h.Set(w, req)

	if store.gotTenant != xtTenantB {
		t.Fatalf("handler accepted tenant from body: got %q, want %q (caller's)",
			store.gotTenant, xtTenantB)
	}
}
