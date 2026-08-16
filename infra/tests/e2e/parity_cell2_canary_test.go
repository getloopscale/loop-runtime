// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Parity: Cell-2 Canary/Rollback — Ginkgo home for the cell-1 canary stage
// procedures migrated from infra/tests/parity/cell2-canary-stage{1,2}.sh
// (Track B, TB2.1 of plan
// docs/plans/completed/2026-05-24-parity-to-ginkgo-migration-plan.md).
//
// TB2.1 port decision (2026-06-05):
//   - The LIVE canary runs (stage 1 = 1h synthetic soak, stage 2 = 24h
//     10%-of-dev-tenants soak) MUTATE tenant placement state and are
//     KEEP-OPERATOR per MEET-ASYNC-AMENDMENT-2026-05-24-001 (ratified).
//   - The Ginkgo port invokes each canonical shell in `--dry-run` mode:
//     validates dependency presence (kubectl/curl/jq), flag parsing
//     (catches regressions like the `--soak-seconds N` while-loop fix),
//     and reachable code paths (preflight, sample selection, pin
//     command construction) WITHOUT mutating tenant placement.
//   - LIVE mutating runs remain operator-only:
//       bash infra/tests/parity/cell2-canary-stage1.sh
//       bash infra/tests/parity/cell2-canary-stage2.sh
package e2e

import (
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parity: Cell-2 Canary/Rollback", Ordered, Label("parity"), func() {
	BeforeAll(func() {
		skipIfNotE2E()
	})

	It("stage1: --dry-run validates preflight + mechanism (LIVE soak is operator-run)", func() {
		// LIVE canary stage 1 pins a synthetic tenant to cell-1 and soaks
		// 1h with HTTP probes + cell-0 EB monitoring. Mutating + long;
		// operator-run via `bash infra/tests/parity/cell2-canary-stage1.sh`.
		// Ginkgo port invokes --dry-run: exercises preflight, flag parse,
		// rollback-trap registration. No tenant mutation.
		repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command("bash",
			strings.TrimSpace(string(repoRoot))+"/infra/tests/parity/cell2-canary-stage1.sh",
			"--dry-run")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"cell2-canary-stage1.sh --dry-run exit non-zero:\n%s", string(out))
		Expect(string(out)).To(ContainSubstring("[dry-run]"),
			"expected --dry-run mode markers in output:\n%s", string(out))
	})

	It("stage2: --dry-run validates preflight + mechanism (LIVE 24h soak is operator-run)", func() {
		// LIVE canary stage 2 pins 10% of dev tenants to cell-1 and soaks
		// 24h. Mutating + very long; operator-run via
		// `bash infra/tests/parity/cell2-canary-stage2.sh`.
		// Ginkgo port invokes --dry-run: exercises preflight, sample
		// selection logic, batch pin command construction. No mutation.
		repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command("bash",
			strings.TrimSpace(string(repoRoot))+"/infra/tests/parity/cell2-canary-stage2.sh",
			"--dry-run")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"cell2-canary-stage2.sh --dry-run exit non-zero:\n%s", string(out))
		Expect(string(out)).To(ContainSubstring("[dry-run]"),
			"expected --dry-run mode markers in output:\n%s", string(out))
	})
})
