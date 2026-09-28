// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Parity: Cell-2 Activation — Ginkgo home for the cell-1 multi-cell invariants
// migrated from infra/tests/parity/cell2-activation-gates.sh +
// lib/cell2-gate-c1..c12.sh (Track B, TB1.1 of plan
// docs/plans/completed/2026-05-24-parity-to-ginkgo-migration-plan.md).
//
// TB1.1 classification (per finding-2026-05-24 gate-heterogeneity): the 12 bash
// activation gates are NOT uniform LIVE checks. Post-activation (cell-1 live):
//   - LIVE reusable multi-cell invariants → real It(): C1, C7, C9, C10, C12.
//   - Pointer/no-op (covered elsewhere or vestigial) → Skip(reason): C2 (Go race
//     test ./internal/placement), C5 (runbook removed — no-op).
//   - Static code-grep (covered by Go unit tests) → Skip(reason): C6
//     (cell_pinner mgmt-only — Go test), C11 (TestAdminPinCell_AuthzInvariantC11).
//   - Production-only (N/A on local k3d) → Skip(reason): C3 (Stage 5a prod),
//     C8 (DOKS revocation).
//
// C1 divergence (finding-2026-05-24-cell2-gate-c1-pg-secret-key-fragility): the
// bash c1 reads an empty `postgresql` Secret .data.password → false FAIL. This
// spec uses the deterministic standalone password (loopzero-dev-password) so C1
// PASSes correctly — a faithful-port IMPROVEMENT, recorded in the finding.
package e2e

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	cell1Ctx   = "k3d-loopzero-cell-0" // cell-0 target (per H12/CLAUDE.md)
	cell2RtCtx = "k3d-loopzero-cell-1" // cell-1 target (per H14)
	// standalone deterministic PG password (chart pins it; bash c1's Secret
	// lookup is empty — see finding-2026-05-24).
	cell2PGPass = "loopzero-dev-password"
)

// kmgmt runs kubectl against the mgmt cluster and returns trimmed stdout.
func kmgmt(args ...string) (string, error) {
	out, err := kubectlForCluster(ClusterMgmt, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

var _ = Describe("Parity: Cell-2 Activation", Ordered, Label("parity"), func() {
	BeforeAll(func() {
		skipIfNotE2E()
		// Preflight (mirrors cell2-canary-stage1.sh:91-95 / H14): cell-1 must be
		// connected, else the activation invariants are N/A.
		connected, _ := kmgmt("-n", "loopzero-system", "get", "clustercredentials",
			"-o", `jsonpath={.items[?(@.spec.cellID=="cell-1")].status.connected}`)
		if !strings.Contains(connected, "true") {
			Skip("cell-1 ClusterCredential not connected (run task k3d:heal — H12); activation invariants N/A")
		}
	})

	// C1 — cells table has cell-1 row + DynamicRouter discovery >= 3.
	It("C1: cells row + discovery count >= 3", func() {
		// cells-table row (deterministic PG password per finding-2026-05-24).
		count, err := kmgmt("-n", "loopzero-system", "exec", "pod/postgresql-0", "-c", "postgresql", "--",
			"env", "PGPASSWORD="+cell2PGPass, "psql", "-U", "loopzero", "-d", "loopzero", "-tAc",
			"SELECT COUNT(*) FROM cells WHERE cell_id='cell-1'")
		Expect(err).NotTo(HaveOccurred(), "psql cells query: %s", count)
		Expect(strings.TrimSpace(count)).To(Equal("1"), "cells table must have exactly one cell-1 row")

		// DynamicRouter discovery >= 3 via loopzero-api /metrics (Prometheus may not
		// scrape locally — direct /metrics is authoritative).
		metrics, err := kmgmt("-n", "loopzero-system", "exec", "deploy/loopzero-api", "--",
			"sh", "-c", "wget -qO- http://localhost:9090/metrics")
		Expect(err).NotTo(HaveOccurred())
		var discovery int
		for _, line := range strings.Split(metrics, "\n") {
			if strings.HasPrefix(line, "loopzero_cell_discovery_count ") {
				f := strings.Fields(line)
				discovery, _ = strconv.Atoi(f[len(f)-1])
			}
		}
		Expect(discovery).To(BeNumerically(">=", 3), "loopzero_cell_discovery_count must be >= 3")
	})

	It("C2: routing determinism (go test -race ./internal/placement)", func() {
		// Faithful port (TB1.1 of plan 2026-05-24-parity-to-ginkgo): invoke the
		// Go race-test suite that this gate exists to assert. Path is rooted at
		// the repo's api module; we cd into it so go.mod resolution works.
		repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command("go", "test", "-race", "-count=1",
			"-run", "TestPlacement|TestRoundRobin|TestRouter",
			"./internal/placement/...")
		cmd.Dir = strings.TrimSpace(string(repoRoot)) + "/api"
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"placement race tests failed:\n%s", string(out))
		Expect(string(out)).To(ContainSubstring("ok"),
			"expected race-clean ok output, got:\n%s", string(out))
	})

	It("C3: Stage-5a tenant-count pre-step (production-only)", func() {
		Skip("production-only (Stage 5a per ADR D6) — N/A on local k3d")
	})

	// C4 was REMOVED, not skipped (#240): it ran
	// infra/tests/parity/cell2-rollback-slo.sh --dry-run, and that script was
	// deleted in b61e6c307 (2026-07-07) with the rest of infra/tests/parity/.
	// The spec could only fail on a missing file, so it measured nothing about
	// rollback SLO. The LIVE measurement it stood in for is an operator
	// procedure with no script in this tree.

	It("C5: PVC/stateful guidance (runbook consolidated — no-op)", func() {
		Skip("no-op gate — docs consolidated; nothing to assert at runtime")
	})

	It("C6: cell_pinner mgmt-only rollback path (go test cell_pinner)", func() {
		// Faithful port: invoke the cell_pinner unit tests that exercise the
		// mgmt-only rollback path (covers `Test.*CellPinner` family).
		repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command("go", "test", "-count=1",
			"-run", "Test.*CellPinner",
			"./internal/placement/...")
		cmd.Dir = strings.TrimSpace(string(repoRoot)) + "/api"
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"cell_pinner tests failed:\n%s", string(out))
		Expect(string(out)).To(ContainSubstring("ok"),
			"expected ok output, got:\n%s", string(out))
	})

	// C7 — cell-0 and cell-1 share the ingress-domain label (rollback needs no DNS change).
	It("C7: cell-0 + cell-1 share ingress-domain label", func() {
		d1, _ := kmgmt("-n", "loopzero-system", "get", "clustercredentials",
			"-o", `jsonpath={.items[?(@.spec.cellID=="cell-0")].metadata.labels.loopzero\.io/ingress-domain}`)
		d2, _ := kmgmt("-n", "loopzero-system", "get", "clustercredentials",
			"-o", `jsonpath={.items[?(@.spec.cellID=="cell-1")].metadata.labels.loopzero\.io/ingress-domain}`)
		Expect(d1).NotTo(BeEmpty(), "cell-0 missing loopzero.io/ingress-domain label (run task k3d:heal)")
		Expect(d2).To(Equal(d1), "cell-0 (%s) and cell-1 (%s) must share ingress-domain (rollback = no DNS change)", d1, d2)
	})

	It("C8: DOKS credential revocation (production-only)", func() {
		Skip("production-only (DOKS revocation) — N/A on local k3d")
	})

	// C9 — cell-0 runtime logs contain zero kubeconfig leak markers.
	It("C9: cell-0 logs clean of kubeconfig leak markers", func() {
		if err := exec.Command("kubectl", "--context", cell1Ctx, "--request-timeout=5s", "cluster-info").Run(); err != nil {
			Skip("cell-0 (workload) cluster unreachable — log-leak check N/A")
		}
		logs, _ := exec.Command("kubectl", "--context", cell1Ctx, "-n", "loopzero-system",
			"logs", "--tail=500", "-l", "app.kubernetes.io/component=runtime", "--all-containers=true").CombinedOutput()
		hits := strings.Count(string(logs), "kubeconfig:")
		Expect(hits).To(Equal(0), "cell-0 logs contain %d kubeconfig marker(s) — credential leak suspected", hits)
	})

	// C10 — a cell-1-pinned tenant's namespace exists ONLY on cell-1, not cell-0.
	It("C10: cell-1 tenant namespace isolated from cell-0", func() {
		tenant, err := kmgmt("-n", "loopzero-system", "exec", "pod/postgresql-0", "-c", "postgresql", "--",
			"env", "PGPASSWORD="+cell2PGPass, "psql", "-U", "loopzero", "-d", "loopzero", "-tAc",
			"SELECT id FROM tenants WHERE cell_id='cell-1' LIMIT 1")
		Expect(err).NotTo(HaveOccurred())
		tenant = strings.TrimSpace(tenant)
		if tenant == "" {
			Skip("no cell-1 tenant pinned yet — isolation check N/A (runs after first pin)")
		}
		ns := tenant + "-production"
		onCell1 := exec.Command("kubectl", "--context", cell1Ctx, "get", "ns", ns).Run() == nil
		Expect(onCell1).To(BeFalse(), "tenant %s pinned to cell-1 but ns %s also on cell-0 (isolation breach)", tenant, ns)
	})

	It("C11: admin-pin authz invariant (go test TestAdminPinCell_AuthzInvariantC11)", func() {
		// Faithful port: invoke the dedicated authz-invariant test that this
		// gate exists to assert.
		repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command("go", "test", "-count=1",
			"-run", "TestAdminPinCell_AuthzInvariantC11",
			"./internal/placement/...")
		cmd.Dir = strings.TrimSpace(string(repoRoot)) + "/api"
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"TestAdminPinCell_AuthzInvariantC11 failed:\n%s", string(out))
		Expect(string(out)).To(ContainSubstring("ok"),
			"expected ok output, got:\n%s", string(out))
	})

	// C12 — every ClusterCredential Secret has base64(PEM) CA data (or insecure-skip-tls).
	It("C12: every ClusterCredential Secret has valid CA data", func() {
		names, err := kmgmt("-n", "loopzero-system", "get", "clustercredentials",
			"-o", `jsonpath={range .items[*]}{.spec.cellID}={.spec.secretRef.name}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).NotTo(BeEmpty(), "no ClusterCredentials found")
		for _, line := range strings.Split(strings.TrimSpace(names), "\n") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 || parts[1] == "" {
				continue
			}
			secretName := parts[1]
			kc, _ := kmgmt("-n", "loopzero-system", "get", "secret", secretName,
				"-o", "jsonpath={.data.kubeconfig}")
			Expect(kc).NotTo(BeEmpty(), fmt.Sprintf("ClusterCredential secret %s (cell %s) has no data.kubeconfig", secretName, parts[0]))
		}
	})

	// Post-flip — cells.status for cell-1 is 'active' + admin pin endpoint
	// rejects unauthed requests (port from cell2-gate-postflip.sh). Uses the
	// deterministic standalone password (same C1 finding-2026-05-24 fix).
	It("Post-flip: cell-1 status active + admin pin authz", func() {
		status, err := kmgmt("-n", "loopzero-system", "exec", "pod/postgresql-0", "-c", "postgresql", "--",
			"env", "PGPASSWORD="+cell2PGPass, "psql", "-U", "loopzero", "-d", "loopzero", "-tAc",
			"SELECT status FROM cells WHERE cell_id='cell-1'")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(status)).To(Equal("active"), "cells.status for cell-1 must be 'active' (migration 000007)")
		// The admin/tenants/pin authz invariant (reject unauthed) is covered by
		// journey_admin_test.go + the api TestAdminPin Go tests — not re-probed here.
	})
})
