// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package servicesacyclic fences the bounded contexts of api/internal/services.
//
// It is a registry over subpackagefence, the engine it shares with
// routesacyclic. The inter-context import graph must stay the acyclic DAG the M9
// split produced, deny-by-default:
//
//	build     → release, scaling
//	release   → (no sibling)
//	scaling   → (no sibling)
//	preview   → (no sibling)
//	domain    → (no sibling)
//	platform  → (no sibling)
//	placement → (no sibling)
//
// The shared types every context needs live in api/internal/deploycontracts,
// OUTSIDE this package, which is why no context needs a sibling to reach them.
//
// Any edge not in the allow-list fails: a reverse edge (release → build) would
// re-create a cycle, a new cross edge erodes the split, and a new context that is
// not registered forces a conscious decision instead of silently widening the
// graph. The registry itself is checked for cycles before the tree is read.
//
// `billing` is deliberately absent: its code moved to loopzero-cloud, and a context
// registered for code that does not exist would pre-approve the decision this
// gate exists to force for whoever recreates the directory.
package servicesacyclic

import (
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
)

const (
	servicesRoot    = "api/internal/services"
	servicesPkgPath = "github.com/getloopscale/loopzero/api/internal/services"
)

// allowed is the deny-by-default registry: allowed[A] is the set of contexts A
// may import.
var allowed = map[string]map[string]bool{
	"build":     {"release": true, "scaling": true},
	"release":   {},
	"scaling":   {},
	"preview":   {},
	"domain":    {},
	"platform":  {},
	"placement": {},
}

// Config controls the gate.
type Config struct {
	RepoRoot string
}

func fence() subpackagefence.Fence {
	return subpackagefence.Fence{Dir: servicesRoot, PkgPath: servicesPkgPath, Allowed: allowed}
}

// Check scans api/internal/services against the registry.
func Check(cfg Config) (subpackagefence.Outcome, error) {
	return subpackagefence.Check(cfg.RepoRoot, fence())
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "An inter-context import edge violates the frozen acyclic DAG of " +
		"api/internal/services (M9 Step 6). Either (a) the shared symbol belongs " +
		"in api/internal/deploycontracts — move it there; or (b) the edge is a " +
		"genuine new dependency that must be reviewed and added to the allow-list " +
		"in servicesacyclic/check.go, which refuses any addition that creates a " +
		"cycle. Reverse edges (e.g. release importing build) are forbidden — they " +
		"re-introduce the cycle the split removed. An unregistered context needs " +
		"the same decision before it may exist."
}
