// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/routesacyclic"
)

func init() {
	register(Gate{
		Name:    "routes-acyclic",
		Summary: "Deny-by-default fence over api/internal/routes subpackages: registered families only, no sibling edges (B-041).",
		Setup:   setupRoutesAcyclic,
	})
}

// setupRoutesAcyclic fences the handler families of api/internal/routes (B-041).
func setupRoutesAcyclic(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := routesacyclic.Check(routesacyclic.Config{RepoRoot: inv.Root})
		return reportSubpackageFence(inv, o, err, routesacyclic.ActionableFix())
	}
}
