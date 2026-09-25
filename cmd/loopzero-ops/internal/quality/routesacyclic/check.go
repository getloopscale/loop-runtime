// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routesacyclic fences the handler families of api/internal/routes.
//
// It is a registry over subpackagefence, the engine it shares with
// servicesacyclic. The fence refuses:
//
//  1. A subpackage of routes that is not registered — a handler family nobody
//     decided on.
//  2. An import between two families. Families are peers; a dependency between
//     them is the coupling the split exists to remove.
//  3. The parent package importing a family, and any family importing the parent
//     — the same coupling on the vertical.
//  4. A production file importing a test-only leaf.
//
// A LEAF is declared, not derived: a family that imports no sibling (`version`)
// has the same empty allow-list as a leaf (`httpio`), so leafness cannot be read
// off the registry. Leaves are what every family, and the parent, may import:
//
//   - httpio holds the response writers every family needs;
//   - pagination holds the request-parsing helpers;
//   - plantier holds what a route does when the tenant's plan cannot be read
//     (billing, projects, scale and status share it);
//   - routestest holds test request helpers. It is TEST-ONLY, because its helpers
//     hand back an owner-role identity for a fixed tenant — a production import
//     of it is auth-shaped.
package routesacyclic

import (
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
)

const (
	routesDir     = "api/internal/routes"
	routesPkgPath = "github.com/getloopscale/loopzero/api/internal/routes"
)

// leaves are the subpackages every family and the parent may import.
var leaves = map[string]bool{
	"httpio":     true,
	"routestest": true,
	"pagination": true,
	"plantier":   true,
}

// testOnlyLeaves may be imported from _test.go files only.
var testOnlyLeaves = map[string]bool{
	"routestest": true,
}

// allowed is the deny-by-default registry: allowed[A] is the set of subpackages A
// may import. Every family is added here when it is split out, so its existence is
// a declaration rather than a discovery.
var allowed = map[string]map[string]bool{
	// leaves
	"httpio":     {},
	"routestest": {},
	"pagination": {},
	"plantier":   {},

	// families
	"version":      {},
	"tenants":      {"httpio": true, "routestest": true},
	"repositories": {"httpio": true, "routestest": true},
	"domains":      {"httpio": true, "routestest": true},
	"secrets":      {"httpio": true, "routestest": true},
	"preview":      {"httpio": true, "routestest": true},
	"billing":      {"httpio": true, "routestest": true, "plantier": true},
	"scale":        {"httpio": true, "routestest": true, "plantier": true},
	"audit":        {"httpio": true, "routestest": true},
	"health":       {"routestest": true},
	"integrations": {"httpio": true, "routestest": true},
	"bootstrap":    {"httpio": true},
	"auth":         {"httpio": true, "routestest": true},
	"teams":        {"httpio": true, "routestest": true},
	"admin":        {"httpio": true},
	"projects":     {"httpio": true, "routestest": true, "pagination": true, "plantier": true},
	"environments": {"httpio": true, "routestest": true, "pagination": true},
	"builds":       {"httpio": true, "routestest": true, "pagination": true},
	"promote":      {"httpio": true, "routestest": true},
	"rollback":     {"httpio": true, "routestest": true},
	"status":       {"httpio": true, "routestest": true, "plantier": true},
	"logs":         {"httpio": true, "routestest": true},
	"metrics":      {"httpio": true},
	"timeline":     {},
	"argocd":       {},
	"source":       {"httpio": true, "routestest": true},
}

// Config controls the gate.
type Config struct {
	RepoRoot string
	// Allowed overrides the registry. Tests only.
	Allowed map[string]map[string]bool
	// Leaves overrides the declared leaf set. Tests only.
	Leaves map[string]bool
}

// Fence returns the routes fence, with the test overrides applied.
func (c Config) Fence() subpackagefence.Fence {
	f := subpackagefence.Fence{
		Dir:            routesDir,
		PkgPath:        routesPkgPath,
		Allowed:        allowed,
		Leaves:         leaves,
		TestOnlyLeaves: testOnlyLeaves,
	}
	if c.Allowed != nil {
		f.Allowed = c.Allowed
	}
	if c.Leaves != nil {
		f.Leaves = c.Leaves
	}
	return f
}

// Check scans api/internal/routes against the registry.
func Check(cfg Config) (subpackagefence.Outcome, error) {
	return subpackagefence.Check(cfg.RepoRoot, cfg.Fence())
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "One of four things:\n" +
		"  · a subpackage nobody registered — decide the family and add it to the\n" +
		"    allow-list with the router prefix it serves;\n" +
		"  · one family reached for another — families are peers;\n" +
		"  · the PARENT package reached for a family, or a family for the parent —\n" +
		"    same coupling, on the vertical, and the usual cause is a helper both need;\n" +
		"  · a PRODUCTION file reached a test-only leaf — routestest is declared\n" +
		"    for _test.go and nothing else, because its helpers mint an\n" +
		"    owner-role identity.\n" +
		"For the coupling cases the fix is the same: move the shared code into a leaf\n" +
		"every family may import, the way httpio holds the response writers.\n" +
		"The test-only case is NOT that fix: move the CALL into a _test.go — or, if\n" +
		"production genuinely needs the helper, move that one helper into a leaf\n" +
		"production may use, which is a decision about identity, not layout."
}
