// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Edge Cases: Multi-Cell Routing (T2.4)
//
// Verifies that workload pods land on the kubectl context corresponding to
// the tenant's pinned cell (placement once, route many — see ADR-0008 +
// ADR-0021). The spec is gated behind cell-2 availability: in single-cell
// k3d setups, the spec is skipped with a clear reason.
//
// Coverage:
//   - matrix #20 (Multi-cell routing) from docs/plans/e2e-ginkgo-rewrite-plan.md
//   - Closes A2 of the post-merge audit (2026-05-06)
//
// What this proves:
//   - tenant.cell_id (canonical SoT in placement table) drives the workload
//     cluster context where the Pod is materialized
//   - Pod for the project appears on the tenant's cell kubectl context
//   - Pod does NOT appear on a different cell's context (isolation invariant)
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

// cell2KubeContextLocal is the local k3d kubectl context for cell-2's workload
// cluster. Per CLAUDE.md the production cell-2 cluster is loopzero-runtime-2; the
// k3d local cluster reuses the same naming convention.
const cell2KubeContextLocal = "k3d-loopzero-runtime-2"

// cell2Available probes whether the cell-2 workload cluster is reachable
// from the host. Used to gate the multi-cell spec — single-cell setups Skip.
func cell2Available() bool {
	cmd := exec.Command("kubectl", "--context", cell2KubeContextLocal,
		"cluster-info", "--request-timeout=3s")
	return cmd.Run() == nil
}

// tenantCellID returns the cell_id assigned to the given tenant by querying
// the database directly via LOOPZERO_E2E_DATABASE_URL.
//
// Why DB and not the admin HTTP API: there is no `GET /api/v1/admin/tenants/{id}`
// endpoint (only POST .../pin and POST .../evict exist). The previous version
// of this helper hit a non-existent path and silently returned "", causing
// the multi-cell spec to fail with "must have a cell_id" even though
// tenants.cell_id was correctly populated. See dogfood-2026-05-08-fixes T3.3
// (NEW, derived from F-4 root-cause investigation).
//
// Returns empty string on any error so callers can fail fast with a clear
// assertion.
func tenantCellID(tenant string) string {
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return ""
	}
	defer conn.Close(ctx)

	var cellID string
	err = conn.QueryRow(ctx, `SELECT cell_id FROM tenants WHERE id = $1`, tenant).Scan(&cellID)
	if err != nil {
		return ""
	}
	return cellID
}

// cellKubeContext maps a cell_id to its local k3d kubectl context.
// cell-2 → cell2KubeContextLocal; everything else (cell-default, cell-1) →
// workloadKubeContext.
func cellKubeContext(cellID string) string {
	if cellID == "cell-2" {
		return cell2KubeContextLocal
	}
	return workloadKubeContext
}

var _ = Describe("Edge: Multi-Cell Routing", Label("edge-case"), func() {
	const (
		project = "solo-server"
		env     = "production"
	)

	var workDir string

	BeforeEach(func() {
		skipIfNotE2E()

		if !cell2Available() {
			Skip(fmt.Sprintf(
				"multi-cell routing requires cell-2 reachable at context %q "+
					"— single-cell setup detected",
				cell2KubeContextLocal,
			))
		}

		seedE2E()
		ensureProjectDeleted(project)

		workDir = copyFixture("solo-server")
		initGitRepo(workDir)

		DeferCleanup(func() {
			ensureProjectDeleted(project)
			if workDir != "" {
				os.RemoveAll(workDir)
			}
		})
	})

	It("deploy lands pod in tenant's pinned cell context", func() {
		// Resolve the cell where the tenant is currently pinned.
		cellID := tenantCellID(seed.TenantID)
		Expect(cellID).NotTo(BeEmpty(),
			"tenant %q must have a cell_id (placement once)", seed.TenantID)

		targetCtx := cellKubeContext(cellID)
		otherCtx := cell2KubeContextLocal
		if targetCtx == cell2KubeContextLocal {
			otherCtx = workloadKubeContext
		}
		GinkgoWriter.Printf("multi-cell: tenant=%s cell_id=%s target_ctx=%s other_ctx=%s\n",
			seed.TenantID, cellID, targetCtx, otherCtx)

		// Deploy and wait for SUCCESS.
		buildID, _ := deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildID).NotTo(BeEmpty(), "deploy must produce a build_id")

		ns := fmt.Sprintf("loopzero-%s-%s", seed.TenantID, env)
		label := fmt.Sprintf("loopzero.io/project=%s", project)

		// Assert pod appears on the tenant's cell context (route many).
		Eventually(func() string {
			cmd := exec.Command("kubectl", "--context", targetCtx,
				"-n", ns, "get", "pods",
				"-l", label, "--ignore-not-found", "-o", "name")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 3*time.Minute, 10*time.Second).Should(
			ContainSubstring("pod/"),
			"project %q pod must appear on cell %q context %q",
			project, cellID, targetCtx,
		)

		// Assert isolation: pod does NOT appear on a different cell context.
		// Namespace may not exist on the other cell — that is a valid pass.
		isoCmd := exec.Command("kubectl", "--context", otherCtx,
			"-n", ns, "get", "pods",
			"-l", label, "--ignore-not-found", "-o", "name")
		isoOut, _ := isoCmd.CombinedOutput()
		Expect(strings.TrimSpace(string(isoOut))).To(BeEmpty(),
			"isolation breach: project %q pod also visible on other cell context %q",
			project, otherCtx,
		)
	})
})
