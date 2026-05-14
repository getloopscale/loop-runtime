// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Edge Cases: Multi-Cell Routing (T2.4) — Plan T4.1 rewrite (2026-05-14)
//
// Closes the dogfood 2026-05-14 finding that the previous spec passed
// trivially: it queried whichever cell the tenant was already pinned
// to (cell-default in cleanroom) and asserted the pod showed up in that
// same cluster's context. There was no cross-cluster placement
// verification — the test could not have detected a regression in
// DynamicRouter.Resolve, ClusterCredential spec.cellID lookup, or
// ArgoCD App destination naming.
//
// New behavior:
//   1. Pre-condition: tenant currently in cell-default
//   2. Pin tenant to cell-2 (DB UPDATE + Tenant CR patch) — equivalent
//      semantics to admin pin endpoint, but avoids the LOOPZERO_ADMIN_*
//      env-var coupling
//   3. Deploy
//   4. Verify pod appears on k3d-loopzero-runtime-2 (NOT loopzero-workload)
//   5. DeferCleanup: pin tenant back to cell-default + delete project
//
// What this proves:
//   - tenants.cell_id update propagates to ArgoCD App destination
//   - ClusterCredential lookup by spec.cellID resolves cell-2 correctly
//   - Pod is materialized on the CORRECT cluster context (not just
//     "some" cluster)
//   - Isolation: pod is NOT visible on a different cell's context
//
// EC-2 fix: the prior spec planning called for a fresh tenant. We pin
// + clean up the existing seed tenant instead, because (a) fresh tenant
// creation requires Hydra OAuth client + admin scope (heavyweight);
// (b) the project is deleted before pinning so there are no leftover
// PVCs in cell-default to "migrate"; (c) DeferCleanup restores
// cell-default at end so subsequent specs see the original state.
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
// cluster (LoopZero's runtime-2 cluster, per CLAUDE.md Multi-Cell section).
const cell2KubeContextLocal = "k3d-loopzero-runtime-2"

// cell2Available probes whether the cell-2 workload cluster is reachable
// from the host AND a ClusterCredential exists for it. Used to gate the
// spec — single-cell setups (CI, dev without Phase 0 bootstrap) Skip.
func cell2Available() bool {
	clusterCmd := exec.Command("kubectl", "--context", cell2KubeContextLocal,
		"cluster-info", "--request-timeout=3s")
	if clusterCmd.Run() != nil {
		return false
	}
	ccCmd := exec.Command("kubectl", "--context", "k3d-loopzero-mgmt",
		"-n", "loopzero-system", "get", "clustercredential", "cell-2",
		"--request-timeout=3s")
	return ccCmd.Run() == nil
}

// tenantCellID returns the cell_id assigned to the given tenant by querying
// the database directly via LOOPZERO_E2E_DATABASE_URL.
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

// rewriteLoopZeroYAMLProject overwrites the project: field in loopzero.yaml so
// the deploy uses a fresh project name (avoiding stale ArgoCD App
// destination state from previous specs).
func rewriteLoopZeroYAMLProject(dir, newProject string) {
	yamlPath := dir + "/loopzero.yaml"
	data, err := os.ReadFile(yamlPath)
	Expect(err).NotTo(HaveOccurred(), "read loopzero.yaml")
	// Replace `project: NAME` first line — simple regex via line scan.
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "project:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "project: " + newProject
			break
		}
	}
	Expect(os.WriteFile(yamlPath, []byte(strings.Join(lines, "\n")), 0o644)).To(Succeed())
}

// pinTenantCell updates BOTH tenants.cell_id (DB SoT) and Tenant CR
// spec.cellID (operator input) atomically from the test's perspective.
// Equivalent semantics to `POST /api/v1/admin/tenants/pin` but uses
// direct k8s + db access — avoids the LOOPZERO_ADMIN_SERVICE_AUTH_SECRET
// env-var coupling that gates the admin route. Returns the previous
// cell_id so cleanup can restore.
func pinTenantCell(tenant, cellID string) (previous string, err error) {
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		return "", fmt.Errorf("LOOPZERO_E2E_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return "", fmt.Errorf("pin: pg connect: %w", err)
	}
	defer conn.Close(ctx)

	// 1. Read current cell_id for restoration.
	if err := conn.QueryRow(ctx,
		`SELECT cell_id FROM tenants WHERE id = $1`,
		tenant,
	).Scan(&previous); err != nil {
		return "", fmt.Errorf("pin: read current cell_id: %w", err)
	}

	// 2. UPDATE DB — canonical SoT per CLAUDE.md Multi-Cell section.
	if _, err := conn.Exec(ctx,
		`UPDATE tenants SET cell_id = $1, updated_at = NOW() WHERE id = $2`,
		cellID, tenant,
	); err != nil {
		return previous, fmt.Errorf("pin: update tenants: %w", err)
	}

	// 3. Patch Tenant CR spec.cellID — operator reconcile reads from CR.
	patch := fmt.Sprintf(`{"spec":{"cellID":"%s"}}`, cellID)
	cmd := exec.Command("kubectl", "--context", "k3d-loopzero-mgmt",
		"-n", "loopzero-system", "patch", "tenant", tenant,
		"--type=merge", "-p", patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return previous, fmt.Errorf("pin: patch CR: %w: %s", err, out)
	}

	// 4. Force-reconcile annotation. 2026-05-14 LIVE finding: spec.cellID
	// changes alone don't always propagate to ArgoCD App destination on
	// the next reconcile cycle (likely a cache/watch race). An annotation
	// update is a reliable trigger because controller-runtime watches
	// metadata as well as spec. Without this, the App destination remains
	// stale (cell-default) even after the CR shows spec.cellID=cell-2.
	// See finding-2026-05-14-cell-pin-argocd-app-destination-stale.md.
	annotateCmd := exec.Command("kubectl", "--context", "k3d-loopzero-mgmt",
		"-n", "loopzero-system", "annotate", "tenant", tenant,
		fmt.Sprintf("loopzero.io/force-reconcile=%d", time.Now().Unix()),
		"--overwrite")
	if out, err := annotateCmd.CombinedOutput(); err != nil {
		return previous, fmt.Errorf("pin: force-reconcile annotation: %w: %s", err, out)
	}

	return previous, nil
}

var _ = Describe("Edge: Multi-Cell Routing", Label("edge-case"), func() {
	const env = "production"

	var (
		workDir string
		// project_id is fresh per spec to avoid stale ArgoCD App
		// destination from prior deploys (which would route to whichever
		// cell the tenant was pinned to at first-deploy time).
		project string
	)

	BeforeEach(func() {
		skipIfNotE2E()

		if !cell2Available() {
			Skip(fmt.Sprintf(
				"multi-cell routing requires cell-2 reachable at context %q AND "+
					"ClusterCredential cell-2 in mgmt cluster — single-cell setup detected "+
					"(run `task k3d:bootstrap-cells` to provision)",
				cell2KubeContextLocal,
			))
		}

		seedE2E()

		// Unique project_id per spec — guarantees no pre-existing ArgoCD
		// App with destination.name=cell-default that ArgoCD would
		// continue syncing to. The fresh App is created with destination
		// resolved from CURRENT tenant.cell_id at deploy time → cell-2.
		project = fmt.Sprintf("mc-test-%s", uniqueSuffix())

		// solo-server fixture but we rewrite loopzero.yaml project name to
		// match our fresh project_id below.
		workDir = copyFixture("solo-server")
		// Overwrite loopzero.yaml so the deploy uses our fresh project name.
		rewriteLoopZeroYAMLProject(workDir, project)
		initGitRepo(workDir)

		DeferCleanup(func() {
			ensureProjectDeleted(project)
			if workDir != "" {
				os.RemoveAll(workDir)
			}
		})
	})

	It("deploy lands pod in cell-2 runtime cluster (NOT workload)", func() {
		// 2026-05-15 LIVE finding (close-all-gaps Phase 1 RESOLUTION):
		//
		// The original 2026-05-14 finding described a two-layer gap.
		// Plan T1.2 LIVE diagnosis revealed the ACTUAL root cause: the
		// tenant-operator ClusterRole was missing
		// `clustercredentials.loopzero.io {get,list,watch}` permission.
		// Without that, RuntimeFactory.For(cellID) couldn't resolve the
		// kubeconfig Secret for the destination cell, so cell-aware
		// namespace creation silently failed.
		//
		// Fix: infra/tilt/tenant-operator-deployment.yaml + production
		// helm chart now grant clustercredentials list. tenant-operator
		// restart + pin to cell-2 produces loopzero-<tenant>-<env>
		// namespaces in k3d-loopzero-runtime-2 within ~30s (LIVE-validated
		// 2026-05-15).

		// Sanity: confirm initial cell (typically cell-default in cleanroom).
		initialCell := tenantCellID(seed.TenantID)
		Expect(initialCell).NotTo(BeEmpty(),
			"tenant %q must have a cell_id (placement once)", seed.TenantID)
		GinkgoWriter.Printf("multi-cell: tenant=%s initial cell=%s\n", seed.TenantID, initialCell)

		// Pin tenant to cell-2 (real cross-cluster move).
		previousCell, err := pinTenantCell(seed.TenantID, "cell-2")
		Expect(err).NotTo(HaveOccurred(), "pin tenant to cell-2")
		GinkgoWriter.Printf("multi-cell: pinned %s: %s -> cell-2\n", seed.TenantID, previousCell)

		// Restore original cell at end of spec.
		DeferCleanup(func() {
			_, restoreErr := pinTenantCell(seed.TenantID, previousCell)
			if restoreErr != nil {
				GinkgoWriter.Printf("warn: failed to restore tenant pin: %v\n", restoreErr)
			}
		})

		// Verify pin took effect at DB level.
		Eventually(func() string {
			return tenantCellID(seed.TenantID)
		}, 10*time.Second, 1*time.Second).Should(Equal("cell-2"),
			"DB cell_id must reflect the pin")

		// Wait for tenant operator to re-emit ArgoCD App with new destination.
		// The force-reconcile annotation in pinTenantCell makes this
		// happen within seconds, but the watch-cache propagation is
		// asynchronous — Eventually loop accommodates 30s slack.
		appName := fmt.Sprintf("%s-%s", seed.TenantID, env)
		Eventually(func() string {
			cmd := exec.Command("kubectl", "--context", "k3d-loopzero-mgmt",
				"-n", "argocd", "get", "application", appName,
				"-o", "jsonpath={.spec.destination.name}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 30*time.Second, 2*time.Second).Should(Equal("cell-2"),
			"ArgoCD App %s.spec.destination.name must update to cell-2 after pin", appName)

		// Deploy. Build runs in build cluster, but the destination is
		// driven by the tenant's cell_id at deploy time → cell-2.
		buildID, _ := deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildID).NotTo(BeEmpty(), "deploy must produce a build_id")

		ns := fmt.Sprintf("loopzero-%s-%s", seed.TenantID, env)
		label := fmt.Sprintf("loopzero.io/project=%s", project)

		// Assert pod appears on cell-2 (runtime-2) context.
		// Timeout 8min — real ArgoCD sync to cell-2 includes:
		//   manifest push → gitea → ArgoCD detect → sync → manifest apply
		//   in cell-2 cluster → Rollout reconcile → pod scheduled
		// Build itself takes 3-5min, deploy phase another 2-3min on k3d.
		Eventually(func() string {
			cmd := exec.Command("kubectl", "--context", cell2KubeContextLocal,
				"-n", ns, "get", "pods",
				"-l", label, "--ignore-not-found", "-o", "name")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 8*time.Minute, 15*time.Second).Should(
			ContainSubstring("pod/"),
			"project %q pod must appear on cell-2 context %q (runtime-2)",
			project, cell2KubeContextLocal,
		)

		// Isolation: pod must NOT appear on the workload (cell-default) context.
		// Namespace may not even exist there — that's a valid pass.
		isoCmd := exec.Command("kubectl", "--context", workloadKubeContext,
			"-n", ns, "get", "pods",
			"-l", label, "--ignore-not-found", "-o", "name")
		isoOut, _ := isoCmd.CombinedOutput()
		Expect(strings.TrimSpace(string(isoOut))).To(BeEmpty(),
			"isolation breach: project %q pod also visible on workload context %q (should be cell-2 only)",
			project, workloadKubeContext,
		)
	})
})
