// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contract

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/getloopscale/loopzero/api/internal/routes"
)

// M1.4 T1.2 — producer conformance.
//
// Two hermetic assertions (no live cluster, no heavy Config):
//
//  1. SpecPathsAreRegistered — every (method, path) documented in engine.yaml is
//     actually registered in api/internal/server/router.go. Catches phantom spec
//     entries + method drift. chi path params use {name} syntax identical to
//     OpenAPI, so no normalization is needed.
//  2. VersionResponseConformsToSpec — a real handler response (the dep-free
//     VersionHandler) validates against the spec via openapi3filter, proving the
//     end-to-end validation wiring. Routes whose handlers require the full engine
//     Config are covered at the routing level (assertion 1) — honest partial
//     coverage per plan ADR D4.

const (
	specPath   = "../../openapi/engine.yaml"
	routerPath = "../server/router.go"
	// routesDir holds helper-registered routes (e.g. RegisterDeployTimelineRoute
	// mounts GET /api/v1/deploys/{deployID}/timeline) that router.go calls but does
	// not spell out as a literal `.Get("...")`. Scanning it too keeps the
	// spec⟺router contract honest for helper-registered routes. Monotonic: it only
	// adds to the registered set, so it can never introduce a new phantom-spec fail.
	routesDir = "../routes"
)

// registeredRouteRE matches chi registrations in either form:
//   r.Get("/api/v1/health", ...)
//   r.With(mw...).Post("/api/v1/builds", ...)   (middleware-wrapped)
// — i.e. any `.<Method>("<path>"` chain, not only the bare `r.<Method>(` form.
var registeredRouteRE = regexp.MustCompile(`\.(Get|Post|Put|Delete|Patch)\(\s*"([^"]+)"`)

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	return doc
}

// registeredRoutes parses router.go and returns the set of "METHOD path" pairs.
func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(routerPath)
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	set := map[string]bool{}
	scan := func(b []byte) {
		for _, m := range registeredRouteRE.FindAllStringSubmatch(string(b), -1) {
			set[strings.ToUpper(m[1])+" "+m[2]] = true
		}
	}
	scan(src)
	// Also scan the routes package for helper-registered routes (non-test .go).
	if entries, derr := os.ReadDir(routesDir); derr == nil {
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			if b, rerr := os.ReadFile(routesDir + "/" + n); rerr == nil {
				scan(b)
			}
		}
	}
	return set
}

func TestEngineConformance_SpecPathsAreRegistered(t *testing.T) {
	doc := loadSpec(t)
	registered := registeredRoutes(t)

	var missing []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			key := method + " " + path
			if !registered[key] {
				missing = append(missing, key)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("engine.yaml documents %d route(s) NOT registered in router.go (phantom spec): %v",
			len(missing), missing)
	}
}

func TestEngineConformance_VersionResponseConformsToSpec(t *testing.T) {
	doc := loadSpec(t)
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("spec invalid: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build router from spec: %v", err)
	}

	srv := httptest.NewServer(routes.VersionHandler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/version", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("spec has no route for GET /api/v1/version: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call version handler: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: resp.StatusCode,
		Header: resp.Header,
	}
	in.SetBodyBytes(body)
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		t.Fatalf("version response does not conform to engine.yaml: %v", err)
	}
}
