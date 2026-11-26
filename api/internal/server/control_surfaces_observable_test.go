// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/server"
	"github.com/getloopscale/loop-runtime/api/internal/surfaces"
)

// TestControlSurfaces_DisabledOnesAreObservableWithAReason is B-115 slice 1: the
// platform's own control paths report whether they were registered.
//
// They were converted first because their absence is invisible from the product
// side. Nobody files a ticket when the ESO broker stops answering — the fleet
// just stops converging — and `secret_fetch` is measured OFF in dev-public right
// now, because the Secret it needs does not exist.
func TestControlSurfaces_DisabledOnesAreObservableWithAReason(t *testing.T) {
	// Routes each surface publishes, to pair the metric with reachability.
	// A metric that disagreed with the router would be worse than no metric.
	routes := map[string]struct{ method, path string }{
		surfaces.Bootstrap:      {http.MethodPost, "/internal/bootstrap"},
		surfaces.ArgoCallback:   {http.MethodPost, "/internal/argocd-callback"},
		surfaces.SecretFetch:    {http.MethodPost, "/internal/v1/secret-fetch/tn-x/SECRET"},
		surfaces.AdminEvict:     {http.MethodPost, "/api/v1/admin/tenants/evict"},
		surfaces.AdminPin:       {http.MethodPost, "/api/v1/admin/tenants/pin"},
		surfaces.ReleaseInspect: {http.MethodGet, "/api/v1/admin/releases/r-1/inspect"},
		surfaces.EdgeRoute:      {http.MethodGet, "/internal/edge/route"},
	}

	reg := surfaces.NewRegistry()
	router := server.NewRouter(server.Config{Surfaces: reg})
	metrics.PublishSurfaces(reg.All())

	// The seven top-level ones. `ActiveTeam` is left out BECAUSE IT IS NESTED: its
	// guard lives inside the authenticated group, which is off in an empty Config, so
	// it is not even evaluated. The series is absent instead of zero — tolerable only
	// because the parent's zero (`auth_protected`) settles the question. See the
	// constant's doc. Asserting here that it is recorded would assert the opposite of
	// what the code does.
	expected := []string{
		surfaces.Bootstrap, surfaces.ArgoCallback, surfaces.SecretFetch,
		surfaces.AdminEvict, surfaces.AdminPin, surfaces.ReleaseInspect,
		surfaces.EdgeRoute, surfaces.AuthProtected,
	}

	if _, recorded := recordedByName(reg)[surfaces.ActiveTeam]; recorded {
		t.Error("`active_team` was recorded with the authenticated group switched off — " +
			"the nesting semantics changed and the constant's doc became false")
	}

	recorded := recordedByName(reg)

	for _, name := range expected {
		s, ok := recorded[name]
		if !ok {
			t.Errorf("surface %q was not recorded — the router decided and discarded the decision", name)
			continue
		}
		if s.Enabled {
			t.Errorf("surface %q reports ENABLED in an empty Config", name)
			continue
		}
		if s.Reason == "" {
			t.Errorf("surface %q is disabled with no reason — whoever sees the zero does not know what to provision", name)
		}
		if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues(name)); got != 0 {
			t.Errorf("metric for %q = %v, want 0", name, got)
		}

		// The route must really be absent. Without this the metric could lie
		// in both directions: report a route that answers as disabled, or the
		// opposite.
		if r, hasRoute := routes[name]; hasRoute {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(r.method, r.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("surface %q reports disabled but %s %s returned %d, want 404",
					name, r.method, r.path, rec.Code)
			}
		}
	}
}

func recordedByName(reg *surfaces.Registry) map[string]surfaces.Surface {
	out := map[string]surfaces.Surface{}
	for _, s := range reg.All() {
		out[s.Name] = s
	}
	return out
}

// TestSecretFetchSurface_EnabledWhenTheHandlerExists proves the enabled half.
//
// `secret_fetch` is the representative because it is the one measured absent in
// dev-public (the `loop-runtime-broker-token` Secret does not exist), and because its
// dependency is a plain http.Handler — provable without stubbing five interfaces
// to assert the same property.
func TestSecretFetchSurface_EnabledWhenTheHandlerExists(t *testing.T) {
	reg := surfaces.NewRegistry()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	router := server.NewRouter(server.Config{Surfaces: reg, Secrets: server.SecretsConfig{FetchHandler: handler}})
	metrics.PublishSurfaces(reg.All())

	if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues(surfaces.SecretFetch)); got != 1 {
		t.Errorf("metric = %v with the handler present, want 1", got)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/internal/v1/secret-fetch/tn-x/SECRET", nil))
	if rec.Code == http.StatusNotFound {
		t.Error("the route returned 404 with the handler present — the metric would be lying")
	}
}

// TestRouter_EachSurfaceUsesADistinctName is structural, and it guards the one
// failure mode the behavioural tests above cannot see.
//
// `Registry.Record` refuses a duplicate name at RUNTIME, and `Recorded` swallows
// that error by design — so a copy-paste that reused a constant would produce a
// router that works, a series that silently reports one surface's state for two,
// and no test failure anywhere. Reading the source is the only place that catch
// can live.
func TestRouter_EachSurfaceUsesADistinctName(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "router.go", nil, 0)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}

	seen := map[string]int{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Recorded" && sel.Sel.Name != "Record") {
			return true
		}
		// The name is the argument right after the registry in `Recorded`, and the
		// first one in `Record`. Picking by position is fragile; looking for the
		// argument that references the `surfaces` package is what identifies the constant.
		for _, arg := range call.Args {
			src := gotypes.ExprString(arg)
			if len(src) > 9 && src[:9] == "surfaces." {
				seen[src]++
			}
		}
		return true
	})

	if len(seen) == 0 {
		t.Fatal("no surface constant found in router.go — either the router " +
			"changed shape, or this test stopped recognising it")
	}
	for constName, n := range seen {
		// Two occurrences are the explicit if/else form (one per branch). Three or
		// more mean a constant reused across distinct surfaces.
		if n > 2 {
			t.Errorf("the constant %s appears %d times — two surfaces sharing a "+
				"name make the series ambiguous, and Recorded swallows the error Record would return",
				constName, n)
		}
	}
}
