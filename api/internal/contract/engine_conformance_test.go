// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contract

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/version/versionhttp"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// M1.4 T1.2 — producer conformance.
//
// Two hermetic assertions (no live cluster, no heavy Config):
//
//  1. SpecPathsAreRegistered — every (method, path) documented in engine.yaml is
//     actually registered by the router (api/internal/server). Catches phantom spec
//     entries + method drift. chi path params use {name} syntax identical to
//     OpenAPI, so no normalization is needed.
//  2. VersionResponseConformsToSpec — a real handler response (the dep-free
//     VersionHandler) validates against the spec via openapi3filter, proving the
//     end-to-end validation wiring. Routes whose handlers require the full engine
//     Config are covered at the routing level (assertion 1) — honest partial
//     coverage per plan ADR D4.

const (
	specPath = "../../openapi/engine.yaml"
	// serverDir holds the router: router.go plus the per-family registrars
	// (protected_routes.go) it delegates to. Every non-test file is scanned,
	// so moving a registration between router files cannot hide it.
	serverDir = "../server"
)

// registeredRouteRE matches chi registrations in either form:
//
//	r.Get("/api/v1/health", ...)
//	r.With(mw...).Post("/api/v1/builds", ...)   (middleware-wrapped)
//
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

// registeredRoutes scans the router's source and returns the set of
// "METHOD path" pairs it registers.
func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	scanTree(t, serverDir, set)
	// The handler packages too, for helper-registered routes
	// (RegisterDeployTimelineRoute mounts GET /api/v1/deploys/{deployID}/timeline
	// without router.go spelling it).
	//
	// WALKED, not listed: when the families were split into subpackages (B-080) a
	// top-level-only listing stopped reaching the helper and reported a registered
	// route as phantom. Scanning more files only ADDS to the registered set, so it
	// can never introduce a new phantom-spec failure.
	for _, dir := range handlerDirs(t) {
		scanTree(t, dir, set)
	}
	return set
}

// scanTree adds every chi registration found in the non-test .go files under
// dir to set. An unreadable tree fails the test: a scan that silently read
// nothing would report every documented route as phantom — or, worse, pass.
func scanTree(t *testing.T, dir string, set map[string]bool) {
	t.Helper()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		n := info.Name()
		if info.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return err
		}
		for _, m := range registeredRouteRE.FindAllStringSubmatch(string(b), -1) {
			set[strings.ToUpper(m[1])+" "+m[2]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s for route registrations: %v", dir, err)
	}
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
		t.Fatalf("engine.yaml documents %d route(s) NOT registered by the router (phantom spec): %v",
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

	srv := httptest.NewServer(versionhttp.VersionHandler())
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
