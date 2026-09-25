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
// A LEAF is declared, not derived: a family that imports no sibling has the same
// empty allow-list as a leaf, so leafness cannot be read off the registry. The
// shared leaves this fence used to declare (httpio, pagination, plantier and the
// test-only routestest) moved to the api/internal kernel when api/internal was
// organized by feature (docs/decisions.md, 2026-09-25), where feature-fence
// governs them; none is left under routes.
package routesacyclic

import (
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
)

const (
	routesDir     = "api/internal/routes"
	routesPkgPath = "github.com/getloopscale/loopzero/api/internal/routes"
)

// leaves are the subpackages every family and the parent may import.
var leaves = map[string]bool{}

// testOnlyLeaves may be imported from _test.go files only.
var testOnlyLeaves = map[string]bool{}

// allowed is the deny-by-default registry: allowed[A] is the set of subpackages A
// may import. Every family is added here when it is split out, so its existence is
// a declaration rather than a discovery.
var allowed = map[string]map[string]bool{
	// families
	"version":      {},
	"tenants":      {},
	"repositories": {},
	"domains":      {},
	"secrets":      {},
	"preview":      {},
	"billing":      {},
	"scale":        {},
	"audit":        {},
	"health":       {},
	"integrations": {},
	"bootstrap":    {},
	"auth":         {},
	"teams":        {},
	"admin":        {},
	"projects":     {},
	"environments": {},
	"builds":       {},
	"promote":      {},
	"rollback":     {},
	"status":       {},
	"logs":         {},
	"metrics":      {},
	"timeline":     {},
	"argocd":       {},
	"source":       {},
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
