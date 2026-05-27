// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Parity: Install Matrix — Ginkgo home for the prod-parity install validation
// migrated from infra/tests/parity/{k3d-virgin-install,preflight,doks-install,
// droplet-install}.sh (Track B, TB3.1 of plan
// docs/plans/completed/2026-05-24-parity-to-ginkgo-migration-plan.md).
//
// SCAFFOLDING (TB0.1): the It bodies Skip() until TB3.1 ports the assertions.
// Cloud (doks/droplet) assertions carry Label("cloud") so local
// --label-filter=parity skips them; only the operator runs parity,cloud.
package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Parity: Install Matrix", Label("parity"), func() {
	BeforeEach(func() {
		skipIfNotE2E()
	})

	It("k3d-virgin: platform converges (port from k3d-virgin-install.sh)", func() {
		Skip("TB3.1 pending — assertions to be ported from infra/tests/parity/k3d-virgin-install.sh")
	})

	It("preflight: environment checks (port from preflight.sh)", func() {
		Skip("TB3.1 pending — assertions to be ported from infra/tests/parity/preflight.sh")
	})

	It("doks: post-install health (port from doks-install.sh)", Label("cloud"), func() {
		Skip("TB3.1 pending — assertions to be ported from infra/tests/parity/doks-install.sh")
	})

	It("droplet: post-install health (port from droplet-install.sh)", Label("cloud"), func() {
		Skip("TB3.1 pending — assertions to be ported from infra/tests/parity/droplet-install.sh")
	})
})
