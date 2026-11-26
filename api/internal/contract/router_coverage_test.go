// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package contract

import (
	"sort"
	"strings"
	"testing"
)

// M11 T1.1 (plan m11-api-validation, DoD "engine.yaml 73%→100%") — the reverse of
// TestEngineConformance_SpecPathsAreRegistered.
//
//	SpecPathsAreRegistered:      every spec (method,path) IS registered in the router (no phantom spec).
//	RouterRoutesAreDocumented:   every registered (method,path) IS documented in the spec (100% coverage).
//
// Together they prove BIJECTIVE coverage — the spec is neither ahead of nor behind
// the router. This gate is hermetic (reuses the regex `registeredRoutes` scan of
// router.go + the routes/ package; no live engine Config), matching the existing
// conformance gate's design.
func TestEngineConformance_RouterRoutesAreDocumented(t *testing.T) {
	doc := loadSpec(t)
	registered := registeredRoutes(t) // "METHOD /api/v1/path" with {param} tokens

	// Spec set of "METHOD /normalized-path" ({param} → {}).
	specSet := map[string]bool{}
	for p, item := range doc.Paths.Map() {
		np := braceParm.ReplaceAllString(p, "{}")
		for method := range item.Operations() {
			specSet[strings.ToUpper(method)+" "+np] = true
		}
	}

	var undocumented []string
	for route := range registered {
		parts := strings.SplitN(route, " ", 2)
		if len(parts) != 2 {
			continue
		}
		path := parts[1]
		// The engine.yaml spec documents the PUBLIC /api/v1 contract surface.
		// Exclude (a) /internal/* service-auth routes (not consumer-facing, out of
		// the published contract by design) and (b) the regex scanner's false
		// positives from `.Get("queryparam")`/`.Header.Get("X")` calls (paths that
		// do not start with /api/v1/).
		if !strings.HasPrefix(path, "/api/v1/") {
			continue
		}
		key := parts[0] + " " + braceParm.ReplaceAllString(path, "{}")
		if !specSet[key] {
			undocumented = append(undocumented, route)
		}
	}
	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Fatalf("router registers %d route(s) NOT documented in engine.yaml (spec coverage < 100%%): %v",
			len(undocumented), undocumented)
	}
}
