// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	mwauth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/release"
)

// Every /api/v1/admin/* route must pass through the ONE admin gate
// (Authenticator.RequireAdminService). The cloud proxy attaches the legacy
// LOOPZERO_SERVICE_AUTH_SECRET to every forwarded user-JWT request, so a route
// that honours that secret lets any signed-in owner reach operator
// primitives (loopzero-cloud#16). The assertion is made on the real router,
// because a handler test passes identically with the route mounted bare.

type stubEvictor struct{}

func (stubEvictor) MatchTenants(context.Context, string, []string) ([]string, error) {
	return []string{"tn-aaaaaaaaaaaaaaaaaaaa"}, nil
}
func (stubEvictor) EnqueueEviction(context.Context, []string) (string, error) { return "job-1", nil }

type stubPinner struct{}

func (stubPinner) Pin(context.Context, string, string) (string, error) { return "cell-0", nil }

type stubInspector struct{}

func (stubInspector) Inspect(context.Context, string) (*release.ReleaseInspection, error) {
	return &release.ReleaseInspection{ReleaseID: "rel-1"}, nil
}

const (
	canonicalAdminSecret = "canonical-admin-secret"
	legacyProxySecret    = "legacy-proxy-secret"
)

type adminRoute struct {
	name, method, path, body string
}

var adminRoutes = []adminRoute{
	{"evict", http.MethodPost, "/api/v1/admin/tenants/evict", `{"ids":["tn-aaaaaaaaaaaaaaaaaaaa"],"dry_run":true}`},
	{"pin", http.MethodPost, "/api/v1/admin/tenants/pin", `{"tenant_id":"tn-x","cell_id":"cell-1"}`},
	{"inspect", http.MethodGet, "/api/v1/admin/releases/rel-1/inspect", ""},
}

func adminRouter(t *testing.T) http.Handler {
	t.Helper()
	return NewRouter(Config{
		Auth: mwauth.NewAuthenticator(mwauth.Config{
			AdminServiceSecret: canonicalAdminSecret,
			ServiceSecret:      legacyProxySecret,
		}),
		Admin: AdminConfig{APIEnabled: true, Evictor: stubEvictor{}, Pinner: stubPinner{}, ReleaseInspector: stubInspector{}},
	})
}

func callAdmin(h http.Handler, rt adminRoute, secret string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
	if secret != "" {
		req.Header.Set("X-Service-Auth", secret)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminRoutes_RefuseTheLegacyProxySecret(t *testing.T) {
	h := adminRouter(t)
	for _, rt := range adminRoutes {
		t.Run(rt.name, func(t *testing.T) {
			rec := callAdmin(h, rt, legacyProxySecret)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s with the proxy secret: want 403, got %d body=%s",
					rt.method, rt.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminRoutes_RefuseAMissingServiceAuthHeader(t *testing.T) {
	h := adminRouter(t)
	for _, rt := range adminRoutes {
		t.Run(rt.name, func(t *testing.T) {
			if rec := callAdmin(h, rt, ""); rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s without X-Service-Auth: want 403, got %d", rt.method, rt.path, rec.Code)
			}
		})
	}
}

func TestAdminRoutes_AdmitTheCanonicalAdminSecret(t *testing.T) {
	h := adminRouter(t)
	for _, rt := range adminRoutes {
		t.Run(rt.name, func(t *testing.T) {
			rec := callAdmin(h, rt, canonicalAdminSecret)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s with the admin secret: want 200, got %d body=%s",
					rt.method, rt.path, rec.Code, rec.Body.String())
			}
		})
	}
}
