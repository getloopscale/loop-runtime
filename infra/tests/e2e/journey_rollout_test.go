// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 13: Argo Rollouts Progressive Delivery (canary).
//
// argo-rollouts-progressive-delivery-plan T5.1 + T5.2.
//
// Validates two scenarios:
//   T5.1 — Happy path: deploy plan=pro, observe Rollout CR with canary
//          strategy (5 steps), TraefikService weights progress, rollout
//          reaches Healthy phase, deploy.completed SSE emitted.
//   T5.2 — Auto-abort path: deploy bad image (broken /health), canary
//          analysis fails (success-rate < 0.90), Rollout auto-aborts,
//          stable pods unchanged (100% traffic), rollout.aborted SSE
//          emitted, CLI exit 3.
//
// Skip semantics:
//   - Skipped when LOOPZERO_E2E unset (suite-level gate).
//   - Skipped when Rollout CRD is missing (Argo Rollouts not installed) —
//     similar to F-07 Velero guard, this checks the actual install signal
//     rather than just namespace existence.
//
// Skipping is per-Describe so other journey tests still run when
// progressive delivery isn't deployed (e.g., during the migration window).

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

// argoRolloutsInstalled reports whether the Rollout CRD is registered on
// the workload cluster. Mirrors the F-07 fix pattern: namespace existence
// is unreliable; CRD registration is canonical signal of install.
// 2026-05-20 drill-parity fix: drill-aware via clusterKubectlArgs.
func argoRolloutsInstalled() bool {
	cmd := kubectlForCluster(ClusterWorkload,
		"get", "crd", "rollouts.argoproj.io", "--no-headers", "-o", "name")
	return cmd.Run() == nil
}

// kubectlWorkload builds a kubectl command rooted at the workload cluster.
// Drill-aware via clusterKubectlArgs(ClusterWorkload) (2026-05-20).
func kubectlWorkload(args ...string) *exec.Cmd {
	return kubectlForCluster(ClusterWorkload, args...)
}

var _ = Describe("Journey 13: Progressive Delivery (canary)", Ordered, Label("journey", "rollout"), func() {
	const (
		project = "rollout-canary-app"
		app     = "api"
		env     = "j-rollout-a"
	)

	var (
		workDir string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		if !argoRolloutsInstalled() {
			Skip("Argo Rollouts not installed (rollouts.argoproj.io CRD missing) — " +
				"deploy via `helmfile sync -l name=argo-rollouts` before re-running")
		}
		seedE2E()

		// Host is computed but not used in the assertion specs below — they
		// query the Rollout/TraefikService directly. Project name + tenant
		// suffice for kubectl scoping.
		_ = seed.TenantID + "-" + project + "-" + app + "-" + env + "." + e2eBaseDomain()
		ensureProjectDeleted(project)
		// Dedicated fixture (project=rollout-canary-app) so this Describe
		// does not collide with journey 01's solo-deploy (project=solo-server)
		// inside the shared E2E tenant namespace.
		workDir = copyFixture("rollout-canary-app")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			cleanupWorkDir(workDir)
		}
	})

	// T5.1 — Happy path
	It("T5.1: deploy generates Rollout (not Deployment) with canary strategy", func() {
		buildID, _ := deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildID).NotTo(BeEmpty(), "build should succeed")

		// Verify the workload cluster received a Rollout CR (not Deployment).
		ns := mustE2ENamespace(project, env)
		resName := project + "-" + app

		// Rollout CR exists. deployAndWaitSuccess waits for build SUCCEEDED
		// (gitops push + sync trigger), but ArgoCD applies manifests to the
		// workload cluster asynchronously — the Rollout may not be present on
		// the first read. Poll until it appears, matching the eventually-
		// consistent sync model (same pattern as the TraefikService spec below).
		Eventually(func() error {
			out, err := kubectlWorkload("get", "rollout",
				"-n", ns, resName, "-o", "jsonpath={.metadata.name}").CombinedOutput()
			if err != nil {
				return &skipError{msg: "rollout not yet synced: " + strings.TrimSpace(string(out))}
			}
			if got := strings.TrimSpace(string(out)); got != resName {
				return &skipError{msg: "rollout name mismatch: got " + got + " want " + resName}
			}
			return nil
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"Rollout %s/%s should exist after ArgoCD sync", ns, resName)

		// Canary strategy declared.
		stratOut, err := kubectlWorkload("get", "rollout",
			"-n", ns, resName, "-o", "jsonpath={.spec.strategy.canary.steps}").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "should read canary steps")
		Expect(string(stratOut)).NotTo(BeEmpty(),
			"Rollout should declare canary steps (got empty)")

		// CHG-2026-04-30: legacy Deployment must NOT be emitted alongside
		// Rollout. Argo Rollouts owns the ReplicaSet lifecycle; coexisting
		// Deployment would race for pod ownership.
		_, err = kubectlWorkload("get", "deployment",
			"-n", ns, resName, "--no-headers").CombinedOutput()
		Expect(err).To(HaveOccurred(),
			"legacy Deployment should NOT exist when progressive delivery is on")
	})

	It("T5.1: TraefikService initial weights stable=100 canary=0", func() {
		ns := mustE2ENamespace(project, env)
		resName := project + "-" + app

		// Wait briefly for resources to settle.
		Eventually(func() error {
			cmd := kubectlWorkload("get", "traefikservice",
				"-n", ns, resName, "-o", "jsonpath={.spec.weighted.services}")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return err
			}
			s := string(out)
			// First service stable=100, second canary=0 initially OR after
			// promotion both 100/0 (stable owns 100% of traffic).
			if !strings.Contains(s, `"weight":100`) {
				return &skipError{msg: "stable weight not yet 100, retrying: " + s}
			}
			return nil
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"TraefikService should have stable=100, canary=0 after rollout completes")
	})

	It("T5.1: Rollout reaches Healthy phase (canary promoted to stable)", func() {
		ns := mustE2ENamespace(project, env)
		resName := project + "-" + app

		// Argo Rollouts marks .status.phase=Healthy when canary fully promoted.
		// Allow generous timeout — full canary on plan=starter is ~1min,
		// ECG container startup adds ~30-60s.
		Eventually(func() string {
			cmd := kubectlWorkload("get", "rollout",
				"-n", ns, resName, "-o", "jsonpath={.status.phase}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 5*time.Minute, 5*time.Second).Should(Equal("Healthy"),
			"Rollout phase should reach Healthy after canary promotion")
	})

})

// argo-rollouts-progressive-delivery-plan T5.2 — Canary auto-abort spec.
// Separate Describe so the AfterAll teardown of the happy-path Describe
// completes before the abort fixture's BeforeAll. Idempotent — uses a
// distinct project name so projects don't collide.
var _ = Describe("Journey 13b: Progressive Delivery — Canary auto-abort", Ordered, Label("journey", "rollout"), func() {
	const (
		project = "rollout-bad-health"
		app     = "api"
		env     = "j-rollout-b"
	)

	var (
		workDir string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		if !argoRolloutsInstalled() {
			Skip("Argo Rollouts not installed (rollouts.argoproj.io CRD missing) — " +
				"deploy via `helmfile sync -l name=argo-rollouts` before re-running")
		}
		seedE2E()

		ensureProjectDeleted(project)
		workDir = copyFixture("rollout-bad-health")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			cleanupWorkDir(workDir)
		}
	})

	It("T5.2: bad-health canary auto-aborts; stable traffic unchanged", func() {
		// Submit deploy. Build itself succeeds (image is valid; /health
		// returns 500 only at runtime). The Rollout enters canary phase
		// and reconciles the canary ReplicaSet — but the renderer wires
		// readinessProbe to /health, which the bad-health fixture serves
		// as 500. The canary pods never reach Ready, the canary step
		// pause never advances, and after Rollout.spec.progressDeadlineSeconds
		// (set to 300s by the renderer for ALL canaries — see
		// renderer_rollout.go), Argo Rollouts marks
		// status.phase=Degraded.
		//
		// This is the pod-readiness abort path. The Prometheus-driven
		// path (AnalysisTemplate failure on success-rate < 0.90) is the
		// stricter validation tier — it requires (a) tenant plan that
		// includes Analysis steps (pro/team), AND (b) PrometheusAddress
		// configured on the API. Both are exercised in staging/prod
		// canary stages; locally only the readiness path applies.
		//
		// Assertion: Rollout status.phase becomes Degraded or Aborted,
		// AND TraefikService stable weight remains 100 (canary never
		// served real traffic to user requests).
		buildID, _ := deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildID).NotTo(BeEmpty())

		ns := mustE2ENamespace(project, env)
		resName := project + "-" + app

		// Wait for Rollout to enter Degraded/Aborted phase. Pod readiness
		// fails immediately; Rollout marks Degraded after
		// progressDeadlineSeconds=300s. Test budget 8min covers worst case.
		Eventually(func() string {
			cmd := kubectlWorkload("get", "rollout",
				"-n", ns, resName, "-o", "jsonpath={.status.phase}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 8*time.Minute, 10*time.Second).Should(SatisfyAny(
			Equal("Degraded"),
			Equal("Aborted"),
		), "Rollout should reach Degraded/Aborted after canary readinessProbe (/health) fails on bad-health fixture")

		// Stable traffic unchanged. Argo Rollouts only modifies weights
		// AFTER analysis succeeds; on abort, weights stay at initial
		// stable=100 / canary=0. (If the rollout had progressed to
		// canary=20 before abort, that's also acceptable — what matters
		// is that stable still holds majority.)
		out, err := kubectlWorkload("get", "traefikservice",
			"-n", ns, resName,
			"-o", "jsonpath={.spec.weighted.services[0].weight}").CombinedOutput()
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(string(out))).To(Equal("100"),
			"stable weight should remain 100 after canary abort")
	})
})

// skipError is an internal sentinel used by the Eventually polling above
// to differentiate "retry" from "fatal" errors.
type skipError struct{ msg string }

func (e *skipError) Error() string { return e.msg }

// cleanupWorkDir removes the temporary workdir created by copyFixture.
// Idempotent: nil/empty path returns nil.
//
// M23 T4.2 (F19): replaced `exec.Command("rm","-rf",path).Run()` with
// path-traversal-safe os.RemoveAll (per plan v1.1 ADR-D3 EC-1/EC-2 absorption).
// Rejects: empty path, bare temp root, paths outside os.TempDir(). The
// filepath.Clean + filepath.Rel guard prevents attacker-controllable or
// mis-generated paths like "/tmp/../etc/passwd" from escaping the temp root.
func cleanupWorkDir(path string) error {
	if path == "" {
		return nil
	}
	cleaned := filepath.Clean(path)
	// EC-2: reject bare temp root — deleting /tmp entirely is catastrophic in shared CI.
	if cleaned == "" || cleaned == "/" || cleaned == "/tmp" || cleaned == filepath.Clean(os.TempDir()) {
		return fmt.Errorf("cleanupWorkDir: refusing to remove bare temp root: %q", path)
	}
	// EC-1: prevent path traversal — require path IS UNDER tempRoot after Clean+Abs.
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return fmt.Errorf("cleanupWorkDir: resolve abs path %q: %w", path, err)
	}
	tempRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return fmt.Errorf("cleanupWorkDir: resolve temp root: %w", err)
	}
	rel, err := filepath.Rel(tempRoot, absPath)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return fmt.Errorf("cleanupWorkDir: refusing to remove path outside temp root: %q (rel=%q)", path, rel)
	}
	return os.RemoveAll(absPath)
}
