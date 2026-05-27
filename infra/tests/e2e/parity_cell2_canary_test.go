// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Parity: Cell-2 Canary/Rollback — Ginkgo home for
// infra/tests/parity/cell2-canary-stage1.sh + cell2-canary-stage2.sh
// (Track B, TB2.1 of plan
// docs/plans/completed/2026-05-24-parity-to-ginkgo-migration-plan.md).
//
// SCAFFOLDING (TB0.1): It bodies Skip() until TB2.1 ports the canary
// weight/health assertions. Per the local known-issue (HTTP 000 in --smoke),
// the port asserts TraefikService weights via kubectl (deterministic), not the
// HTTP probe — same approach as the journey-13 fix.
package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Parity: Cell-2 Canary/Rollback", Ordered, Label("parity"), func() {
	BeforeAll(func() {
		skipIfNotE2E()
		Skip("TB2.1 pending — port from cell2-canary-stage1/2.sh")
	})

	It("stage1: canary weight progresses (port from cell2-canary-stage1.sh)", func() {
		Skip("TB2.1 pending")
	})

	It("stage2: canary promotion + rollback SLO (port from cell2-canary-stage2.sh)", func() {
		Skip("TB2.1 pending")
	})
})
