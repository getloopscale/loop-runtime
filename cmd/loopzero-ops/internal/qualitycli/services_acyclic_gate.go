// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/servicesacyclic"
)

func init() {
	register(Gate{
		Name:    "services-acyclic",
		Summary: "M9 Step 6: services bounded-context imports form the frozen acyclic DAG.",
		Setup:   setupServicesAcyclic,
	})
}

// setupServicesAcyclic enforces the M9 Step 6 bounded-context acyclic import DAG of
// api/internal/services (deny-by-default allow-list). ADR
// 2026-07-03-m9-db-store-subpackage-topology; MEET-2026-07-03-001.
func setupServicesAcyclic(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := servicesacyclic.Check(servicesacyclic.Config{RepoRoot: inv.Root})
		return reportSubpackageFence(inv, outcome, err, servicesacyclic.ActionableFix())
	}
}
