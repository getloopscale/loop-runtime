// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Parity: Install Matrix — Ginkgo home for the prod-parity install validation
// migrated from infra/tests/parity/{k3d-virgin-install,preflight,doks-install,
// droplet-install}.sh (Track B, TB3.1 of plan
// docs/plans/completed/2026-05-24-parity-to-ginkgo-migration-plan.md).
//
// TB3.1 status (2026-06-05):
//   - preflight: PORTED — invokes the canonical shell as a sub-runner so the
//     assertion is visible in the Ginkgo report.
//   - k3d-virgin: kept as documented SKIP — destrutivo (pauses live build +
//     workload clusters, creates virgin cluster, runs helmfile sync, tears
//     down), stand-alone validator (operator-run via `task drill:*`).
//   - doks/droplet: kept as documented SKIP — Label("cloud") + sponsor
//     mandate 2026-05-26 "naos vamos provisionar k8s por custos" —
//     permanently N/A em dev-public.
package e2e

import (
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parity: Install Matrix", Label("parity"), func() {
	BeforeEach(func() {
		skipIfNotE2E()
	})

	It("k3d-virgin: platform converges (operator-run, destructive)", func() {
		// k3d-virgin-install.sh pauses live loopzero-build + loopzero-cell-0 to
		// free RAM, creates `loopzero-virgin`, runs full helmfile sync, runs
		// journey-01 smoke, then tears the virgin cluster down + restarts
		// the originals. Wrong scope for inline dogfood: stand-alone by
		// design. Operator runs it via `task drill:*` or manually.
		Skip("destructive (pauses clusters + recreates) — operator-run via " +
			"`bash infra/tests/parity/k3d-virgin-install.sh`")
	})

	It("preflight: environment checks (invokes canonical preflight.sh)", func() {
		// Faithful port (TB3.1): invoke the canonical shell directly. The
		// shell is the source of truth; this wrapper makes the assertion
		// visible in the Ginkgo report.
		repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command("bash",
			strings.TrimSpace(string(repoRoot))+"/infra/tests/parity/preflight.sh")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"parity/preflight.sh exit non-zero:\n%s", string(out))
	})

	It("doks: post-install health (cloud — sponsor: no K8s cost)", Label("cloud"), func() {
		// Sponsor mandate 2026-05-26: "naos vamos provisionar k8s por
		// custos". DOKS provisioning is permanently N/A on dev-public.
		Skip("cloud-only (DOKS) — sponsor mandate: no cloud K8s")
	})

	It("droplet: post-install health (cloud — sponsor: no K8s cost)", Label("cloud"), func() {
		Skip("cloud-only (DO droplet) — sponsor mandate: no cloud K8s")
	})
})
