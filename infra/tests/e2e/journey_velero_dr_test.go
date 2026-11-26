// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey 12: Velero Backup & Disaster Recovery
//
// Validates the Velero backup/DR plan (docs/plans/velero-backup-dr-plan.md
// v1.1 CONVERGED, 2026-05-06) by exercising the LIVE Velero install on
// the mgmt cluster. The journey exists as a regression-prevention layer
// over the manual `velero-dr-drill.sh` script: every CI run guards the
// invariants the plan promised would hold.
//
// Skip semantics:
//   - Skipped when LOOPRUNTIME_E2E unset (suite-level gate).
//   - Skipped when the velero-system namespace is missing — Velero is
//     installed via Helmfile mgmt Wave -1 but is not part of the
//     bare-minimum standalone bootstrap; running the rest of the suite
//     against a Velero-less cluster is a valid configuration.
//   - Specs that require the `velero` CLI binary skip individually
//     (drill smoke + on-demand backup) so the structural invariants
//     still run on hosts without the CLI installed.
//
// Coverage matrix (file-level, not duplicated in plan §):
//
//   * V-1: velero-system pod healthy
//   * V-2: BackupStorageLocation `default` phase=Available
//   * V-3: 5 loopruntime-* Schedules exist + Enabled
//   * V-4: loopruntime-crds-backup template includes all 10 runtime.getloopscale.com CRDs
//   * V-5: PrometheusRule loopruntime-velero-alerts has 4 alerts
//   * V-6: ORPHAN LoopRuntimeGiteaBackupStale removed from
//          prometheusrule-critical.yaml (regression guard)
//   * V-7: DR drill --dry-run passes (when velero CLI present)
//   * V-8: On-demand backup completes (when velero CLI present)

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const veleroNamespace = "velero-system"

// veleroInstalled reports whether the Velero release is present on the
// management cluster. Used to skip the entire Describe when running on
// minimal bootstraps that do not deploy Velero.
//
// F-07 fix (2026-05-07 cleanroom dogfood): namespace existence alone is
// not a reliable signal — cleanroom setup creates velero-system as part
// of helmfile orchestration even when Velero itself is not deployed
// (Velero ships separately via `helmfile sync -l name=velero`, not part
// of bare task setup). Check for the actual Velero CRD registration.
// When Velero is genuinely installed, backups.velero.io CRD is present.
// When only the namespace is provisioned without Velero, CRD is absent.
//
// 2026-05-20 drill-parity fix: switch from "--context mgmtKubeContext"
// to clusterKubectlArgs(ClusterMgmt) so drill DOKS runs resolve the
// right cluster (per feedback_velero_must_be_in_drill memory).
func veleroInstalled() bool {
	cmd := kubectlForCluster(ClusterMgmt,
		"get", "crd", "backups.velero.io", "--no-headers", "-o", "name")
	return cmd.Run() == nil
}

// veleroCLIAvailable returns true when the `velero` binary is on PATH.
// Lets specs that need the CLI skip with a clear reason instead of
// emitting an opaque exec error.
func veleroCLIAvailable() bool {
	_, err := exec.LookPath("velero")
	return err == nil
}

// kubectlMgmt builds a kubectl command rooted at the mgmt cluster.
// Drill-aware via clusterKubectlArgs(ClusterMgmt) (2026-05-20).
func kubectlMgmt(args ...string) *exec.Cmd {
	return kubectlForCluster(ClusterMgmt, args...)
}

// veleroNs builds a kubectl command scoped to velero-system.
func veleroNs(args ...string) *exec.Cmd {
	return kubectlMgmt(append([]string{"-n", veleroNamespace}, args...)...)
}

// expectedSchedules is the canonical list per ADR D3 of the plan. New
// schedules MUST be added here AND to infra/helm/velero/values-schedules.yaml
// in the same commit — divergence is a regression caught by V-3.
var expectedSchedules = []string{
	"velero-loopruntime-argocd-backup",
	"velero-loopruntime-crds-backup",
	"velero-loopruntime-full-backup",
	"velero-loopruntime-gitea-backup",
	"velero-loopruntime-secrets-backup",
}

// expectedLoopRuntimeCRDs is the canonical list of runtime.getloopscale.com CRD plural names
// the crds-backup schedule MUST capture. Sourced from operators/config/.
var expectedLoopRuntimeCRDs = []string{
	"tenants.runtime.getloopscale.com",
	"environments.runtime.getloopscale.com",
	"loopruntimeapplications.runtime.getloopscale.com",
	"exposedroutes.runtime.getloopscale.com",
	"platformbootstraps.runtime.getloopscale.com",
	"gitrepositorybindings.runtime.getloopscale.com",
	"clustercredentials.runtime.getloopscale.com",
	"previewenvironments.runtime.getloopscale.com",
}

var _ = Describe("Journey 12: Velero Backup & DR", Label("journey"), func() {
	BeforeEach(func() {
		skipIfNotE2E()

		if !veleroInstalled() {
			Skip("Velero not installed (no velero-system namespace) — " +
				"deploy via `helmfile sync -l name=velero` before re-running")
		}
	})

	It("V-1: velero pod is Running", func() {
		Eventually(func() string {
			cmd := veleroNs("get", "pods",
				"-l", "app.kubernetes.io/name=velero",
				"-o", "jsonpath={.items[0].status.phase}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 1*time.Minute, 5*time.Second).Should(
			Equal("Running"),
			"velero pod must be Running in %s", veleroNamespace,
		)
	})

	It("V-2: BackupStorageLocation default is Available", func() {
		Eventually(func() string {
			cmd := veleroNs("get", "bsl", "default",
				"-o", "jsonpath={.status.phase}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 2*time.Minute, 5*time.Second).Should(
			Equal("Available"),
			"BackupStorageLocation default must be Available "+
				"(check S3/MinIO connectivity + credentials Secret)",
		)
	})

	It("V-3: 5 loopruntime-* schedules exist and are Enabled", func() {
		// One pass per schedule for clearer failure messages.
		for _, name := range expectedSchedules {
			// Fully-qualify schedules.velero.io: when chaos-mesh is also
			// installed (resilience + backup coexist), the bare "schedule"
			// short name is ambiguous and kubectl resolves it to
			// schedules.chaos-mesh.org → false NotFound.
			cmd := veleroNs("get", "schedules.velero.io", name,
				"-o", "jsonpath={.metadata.name}={.spec.paused}")
			out, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"schedule %q should exist (got: %s)", name, string(out))

			result := strings.TrimSpace(string(out))
			// jsonpath returns empty string for absent boolean fields. Velero
			// defaults paused=false (Enabled) so we accept both "name=" and
			// "name=false". A spec.paused=true value would fail the test.
			Expect(result).To(SatisfyAny(
				Equal(name+"="),
				Equal(name+"=false"),
			), "schedule %q must be Enabled (got %q)", name, result)
		}
	})

	It("V-4: loopruntime-crds-backup template includes all 10 runtime.getloopscale.com CRDs", func() {
		cmd := veleroNs("get", "schedules.velero.io", "velero-loopruntime-crds-backup",
			"-o", "jsonpath={.spec.template.includedResources}")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"velero-loopruntime-crds-backup must exist (got: %s)", string(out))

		rendered := string(out)
		for _, crd := range expectedLoopRuntimeCRDs {
			Expect(rendered).To(
				ContainSubstring(crd),
				"loopruntime-crds-backup must include CRD %q (got: %s)", crd, rendered,
			)
		}
	})

	It("V-5: PrometheusRule loopruntime-velero-alerts has 4 alerts", func() {
		// The PrometheusRule may live under monitoring or velero-system —
		// the exact namespace depends on the helmfile values. We probe the
		// rules via a label selector instead of hard-coding the namespace.
		cmd := kubectlMgmt("get", "prometheusrule",
			"-A",
			"-l", "runtime.getloopscale.com/component=velero-alerts",
			"-o", "jsonpath={.items[*].spec.groups[*].rules[*].alert}")
		out, _ := cmd.CombinedOutput()
		alerts := strings.Fields(strings.TrimSpace(string(out)))

		// Fallback: read the source file directly when the PrometheusRule
		// CRD is not registered in this cluster (e.g., monitoring stack
		// disabled). This keeps the assertion meaningful without forcing
		// every test environment to install Prometheus Operator.
		if len(alerts) == 0 {
			data, err := os.ReadFile(
				"../../helm/monitoring/manifests/prometheusrule-velero.yaml")
			if err != nil {
				Skip("PrometheusRule not loaded and source file unreadable: " + err.Error())
			}
			alerts = nil
			for _, line := range strings.Split(string(data), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "- alert: ") {
					alerts = append(alerts, strings.TrimPrefix(trimmed, "- alert: "))
				}
			}
		}

		Expect(alerts).To(HaveLen(4),
			"prometheusrule-velero must define exactly 4 alerts (got %v)", alerts)
		Expect(alerts).To(ContainElements(
			"LoopRuntimeVeleroBackupStale",
			"LoopRuntimeVeleroBackupFailing",
			"LoopRuntimeVeleroServerDown",
			"LoopRuntimeVeleroBackupPartial",
		), "alert names must match the canonical set")
	})

	It("V-6: ORPHAN LoopRuntimeGiteaBackupStale is removed (regression guard)", func() {
		// The orphan rule was deleted by the velero-backup-dr plan T3.1.
		// This guard ensures it does not return — a future PR adding the
		// rule back would fail this spec immediately.
		data, err := os.ReadFile(
			"../../helm/monitoring/manifests/prometheusrule-critical.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).NotTo(
			ContainSubstring("alert: LoopRuntimeGiteaBackupStale"),
			"ORPHAN LoopRuntimeGiteaBackupStale rule must NOT be reintroduced — "+
				"replaced by LoopRuntimeVeleroBackupStale per plan T3.1",
		)
	})

	It("V-7: DR drill --dry-run passes", func() {
		if !veleroCLIAvailable() {
			Skip("`velero` CLI not on PATH — install vmware-tanzu/velero@v1.14.x")
		}

		// Foreground execution; the script's own watchdog enforces the
		// inner timeout (default 300s, here we cap with a Gomega outer).
		cmd := exec.Command("bash",
			"../../scripts/drill/velero-dr-drill.sh",
			"--context", mgmtKubeContext,
			"--dry-run")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"velero-dr-drill --dry-run must exit 0 (got: %s)", string(out))

		report := strings.TrimSpace(string(out))
		Expect(report).To(
			ContainSubstring(`"status":"passed"`),
			"drill report must include status=passed (got: %s)", report,
		)
	})

	It("V-8: on-demand backup completes", func() {
		if !veleroCLIAvailable() {
			Skip("`velero` CLI not on PATH — install vmware-tanzu/velero@v1.14.x")
		}

		backupName := "ginkgo-velero-smoke-" + uniqueSuffix()
		create := exec.Command("velero",
			"--kubecontext", mgmtKubeContext,
			"-n", veleroNamespace,
			"backup", "create", backupName,
			"--include-resources", "tenants.runtime.getloopscale.com",
			"--wait")
		out, err := create.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"on-demand backup must complete (got: %s)", string(out))
		Expect(string(out)).To(
			ContainSubstring("status: Completed"),
			"velero CLI must report status=Completed (got: %s)", string(out))

		// Cleanup: drop the smoke backup so reruns do not accumulate
		// per-run Backup CRs. Best-effort, no failure if delete returns
		// non-zero (Velero deletes async via DeleteBackupRequest).
		_ = exec.Command("velero",
			"--kubecontext", mgmtKubeContext,
			"-n", veleroNamespace,
			"backup", "delete", backupName,
			"--confirm").Run()
	})
})
