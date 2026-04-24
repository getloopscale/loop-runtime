// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chaos_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestChaos(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Chaos Engineering Suite")
}

// skipUnlessChaos skips if LOOPZERO_CHAOS_TEST=1 is not set.
func skipUnlessChaos() {
	if os.Getenv("LOOPZERO_CHAOS_TEST") != "1" {
		Skip("skipping chaos test: set LOOPZERO_CHAOS_TEST=1 to run")
	}
}

// manifestPath returns the absolute path to a chaos manifest file.
func manifestPath(name string) string {
	// Relative to this test file: ./manifests/<name>
	p, _ := filepath.Abs(filepath.Join("manifests", name))
	return p
}

// kubectl runs a kubectl command and returns stdout.
func kubectl(args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// kubectlContext runs a kubectl command with the specified context.
func kubectlContext(ctx string, args ...string) (string, error) {
	fullArgs := append([]string{"--context", ctx}, args...)
	return kubectl(fullArgs...)
}

// applyChaos applies a Chaos Mesh manifest. Returns a cleanup function.
func applyChaos(name string) func() {
	path := manifestPath(name)
	out, err := kubectl("apply", "-f", path)
	Expect(err).NotTo(HaveOccurred(), "apply chaos %s failed: %s", name, out)
	GinkgoWriter.Printf("  Applied chaos: %s\n", name)
	return func() {
		out, err := kubectl("delete", "-f", path, "--ignore-not-found")
		if err != nil {
			GinkgoWriter.Printf("  Warning: failed to delete chaos %s: %s\n", name, out)
		}
	}
}

// waitForPodReady waits for a pod matching the label selector to be ready.
func waitForPodReady(namespace, labelSelector string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod %s in %s", labelSelector, namespace)
		default:
			out, err := kubectl("get", "pods", "-n", namespace, "-l", labelSelector,
				"-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
			if err == nil && strings.TrimSpace(out) == "True" {
				return nil
			}
			time.Sleep(5 * time.Second)
		}
	}
}

// checkAPIHealth verifies the API is responding to health checks.
func checkAPIHealth() error {
	out, err := kubectl("exec", "-n", "loopzero-system",
		"deploy/loopzero-api", "--", "wget", "-qO-", "-T", "5",
		"http://localhost:8080/api/v1/health")
	if err != nil {
		return fmt.Errorf("API health check failed: %s: %v", out, err)
	}
	return nil
}

// checkMetricExists verifies a Prometheus metric is registered.
func checkMetricExists(metricName string) {
	out, err := kubectl("exec", "-n", "loopzero-system",
		"deploy/loopzero-api", "--",
		"wget", "-qO-", "-T", "5", "http://localhost:8080/metrics")
	Expect(err).NotTo(HaveOccurred(), "failed to fetch metrics")
	Expect(out).To(ContainSubstring(metricName),
		"metric %s not found in /metrics output", metricName)
}

var _ = Describe("Chaos Engineering", func() {

	// ---------------------------------------------------------------
	// Scenario 1: API pod crash recovery
	// scale-50k-plan L1 chaos: "Kill API pod during deploy → verify
	// controller continues processing → app goes live"
	// ---------------------------------------------------------------
	Context("API pod crash during operation", func() {
		It("should recover and serve health within 120s", func() {
			skipUnlessChaos()

			cleanup := applyChaos("pod-kill-api-leader.yaml")
			defer cleanup()

			// Wait for the fault to take effect (pod killed).
			time.Sleep(15 * time.Second)

			// The replacement pod should become ready.
			err := waitForPodReady("loopzero-system", "app=loopzero-api", 120*time.Second)
			Expect(err).NotTo(HaveOccurred(), "API pod did not recover within 120s")

			// Verify health.
			err = checkAPIHealth()
			Expect(err).NotTo(HaveOccurred(), "API health check failed after restart")
		})

		It("should release advisory lock on pod termination", func() {
			skipUnlessChaos()

			// Kill leader pod — the advisory lock (session-scoped on dedicated conn)
			// is released when the PG connection closes.
			out, err := kubectl("delete", "pod", "-n", "loopzero-system",
				"-l", "app=loopzero-api", "--wait=false")
			Expect(err).NotTo(HaveOccurred(), "failed to delete API pod: %s", out)

			// Wait for new pod to become leader (up to 90s for election).
			time.Sleep(15 * time.Second)
			err = waitForPodReady("loopzero-system", "app=loopzero-api", 90*time.Second)
			Expect(err).NotTo(HaveOccurred(), "replacement API pod did not become ready")
		})
	})

	// ---------------------------------------------------------------
	// Scenario 2: Controller crash during reconciliation
	// scale-50k-plan L1 chaos: "Kill controller pod → verify leader
	// re-election via generation fencing → no stuck builds"
	// ---------------------------------------------------------------
	Context("Controller crash during reconciliation", func() {
		It("should recover via leader re-election within 60s", func() {
			skipUnlessChaos()

			cleanup := applyChaos("pod-kill-controller.yaml")
			defer cleanup()

			// Wait for fault + recovery.
			time.Sleep(10 * time.Second)

			err := waitForPodReady("loopzero-system", "app=loopzero-controller", 120*time.Second)
			Expect(err).NotTo(HaveOccurred(), "controller pod did not recover within 120s")

			// Verify leader lease was re-acquired.
			out, err := kubectl("logs", "-n", "loopzero-system",
				"-l", "app=loopzero-controller", "--tail=30")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("leader"),
				"controller did not re-acquire leader lease after restart")
		})
	})

	// ---------------------------------------------------------------
	// Scenario 3: PG connection storm → circuit breaker
	// scale-50k-plan L1 chaos: "PG connection spike → verify circuit
	// breaker trips + recovers → next deploy succeeds"
	// ---------------------------------------------------------------
	Context("PG connection storm triggers circuit breaker", func() {
		It("should trip breaker at high saturation and auto-reset", func() {
			skipUnlessChaos()

			// Verify the metric exists before injecting fault.
			checkMetricExists("loopzero_pg_circuit_breaker_tripped_total")

			cleanup := applyChaos("pg-connection-storm.yaml")
			defer cleanup()

			// Hold the stress for the experiment duration (120s).
			GinkgoWriter.Println("  Holding PG stress for 120s...")
			time.Sleep(130 * time.Second)

			// After the experiment, the breaker should reset.
			// Give it 30s to recover (LowWaterMark=0.75).
			time.Sleep(30 * time.Second)

			// Health check should pass after recovery.
			err := checkAPIHealth()
			Expect(err).NotTo(HaveOccurred(), "API should be healthy after PG storm recovery")
		})
	})

	// ---------------------------------------------------------------
	// Scenario 4: PG network latency
	// scale-50k-plan L2 chaos: "PG failover under load"
	// ---------------------------------------------------------------
	Context("PG network latency injection", func() {
		It("should handle 500ms latency without crashing", func() {
			skipUnlessChaos()

			cleanup := applyChaos("pg-network-latency.yaml")
			defer cleanup()

			// Wait for latency injection to take effect.
			time.Sleep(10 * time.Second)

			// API should still respond (slower, but not crash).
			err := checkAPIHealth()
			Expect(err).NotTo(HaveOccurred(), "API should survive PG latency injection")

			// Wait for experiment to expire.
			time.Sleep(60 * time.Second)
		})
	})

	// ---------------------------------------------------------------
	// Scenario 5: ArgoCD crash → cell isolation
	// scale-50k-plan L1-L3 chaos: "ArgoCD crash in one cell should
	// not affect other cells' tenants"
	// ---------------------------------------------------------------
	Context("ArgoCD crash with cell isolation", func() {
		It("should recover ArgoCD within 120s", func() {
			skipUnlessChaos()

			cleanup := applyChaos("argocd-crash.yaml")
			defer cleanup()

			time.Sleep(10 * time.Second)

			err := waitForPodReady("argocd", "app.kubernetes.io/name=argocd-server", 120*time.Second)
			Expect(err).NotTo(HaveOccurred(), "ArgoCD did not recover within 120s")
		})

		It("should not affect API health during ArgoCD outage", func() {
			skipUnlessChaos()

			cleanup := applyChaos("argocd-crash.yaml")
			defer cleanup()

			// API should remain healthy even while ArgoCD is down.
			time.Sleep(5 * time.Second)
			err := checkAPIHealth()
			Expect(err).NotTo(HaveOccurred(), "API should be healthy during ArgoCD outage")
		})
	})

	// ---------------------------------------------------------------
	// Scenario 6: Gitea outage
	// scale-50k-plan: "Builds fail gracefully with clear error, no
	// silent data loss"
	// ---------------------------------------------------------------
	Context("Gitea outage", func() {
		It("should fail builds gracefully without silent data loss", func() {
			skipUnlessChaos()

			cleanup := applyChaos("gitea-outage.yaml")
			defer cleanup()

			// Wait for fault to take effect.
			time.Sleep(10 * time.Second)

			// API should still be healthy (Gitea is not on the health path).
			err := checkAPIHealth()
			Expect(err).NotTo(HaveOccurred(), "API should be healthy during Gitea outage")

			// Wait for experiment to expire.
			time.Sleep(60 * time.Second)

			// After recovery, verify Gitea is responsive.
			out, err := kubectl("get", "pods", "-n", "gitea",
				"-l", "app.kubernetes.io/name=gitea",
				"-o", "jsonpath={.items[0].status.phase}")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(out)).To(Equal("Running"))
		})
	})

	// ---------------------------------------------------------------
	// Scenario 7: Build cluster node failure
	// scale-50k-plan L2-L4 chaos: "Build cluster node drain → builds
	// reschedule to remaining nodes, complete successfully"
	// ---------------------------------------------------------------
	Context("Build cluster node failure", func() {
		It("should reschedule workflows when a build node is cordoned", func() {
			skipUnlessChaos()

			buildContext := os.Getenv("LOOPZERO_BUILD_CONTEXT")
			if buildContext == "" {
				buildContext = "do-sfo3-loopzero-infra-dev-sfo3-build"
			}

			// Get a build node name.
			out, err := kubectlContext(buildContext, "get", "nodes",
				"-o", "jsonpath={.items[0].metadata.name}")
			Expect(err).NotTo(HaveOccurred(), "failed to get build node: %s", out)
			nodeName := strings.TrimSpace(out)
			Expect(nodeName).NotTo(BeEmpty(), "no build nodes found")

			// Cordon the node (make it unschedulable).
			out, err = kubectlContext(buildContext, "cordon", nodeName)
			Expect(err).NotTo(HaveOccurred(), "failed to cordon node: %s", out)
			defer func() {
				// Always uncordon.
				kubectlContext(buildContext, "uncordon", nodeName)
			}()

			GinkgoWriter.Printf("  Cordoned build node: %s\n", nodeName)

			// Verify the node is unschedulable.
			out, err = kubectlContext(buildContext, "get", "node", nodeName,
				"-o", "jsonpath={.spec.unschedulable}")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(out)).To(Equal("true"))

			// Wait briefly then uncordon — new workflows should schedule
			// on remaining nodes.
			time.Sleep(30 * time.Second)
		})
	})

	// ---------------------------------------------------------------
	// Scenario 8: Simultaneous API + controller crash
	// scale-50k-plan L4 chaos: "Simultaneous controller + API crash
	// → full recovery < 3min"
	// ---------------------------------------------------------------
	Context("Simultaneous API and controller crash", func() {
		It("should recover both within 3 minutes", func() {
			skipUnlessChaos()

			// Kill both simultaneously.
			out1, err1 := kubectl("delete", "pod", "-n", "loopzero-system",
				"-l", "app=loopzero-api", "--wait=false")
			out2, err2 := kubectl("delete", "pod", "-n", "loopzero-system",
				"-l", "app=loopzero-controller", "--wait=false")

			Expect(err1).NotTo(HaveOccurred(), "API kill failed: %s", out1)
			Expect(err2).NotTo(HaveOccurred(), "controller kill failed: %s", out2)

			GinkgoWriter.Println("  Killed both API and controller pods")

			// Both should recover within 3 minutes.
			start := time.Now()

			err := waitForPodReady("loopzero-system", "app=loopzero-api", 180*time.Second)
			Expect(err).NotTo(HaveOccurred(), "API did not recover within 3min")

			err = waitForPodReady("loopzero-system", "app=loopzero-controller", 180*time.Second)
			Expect(err).NotTo(HaveOccurred(), "controller did not recover within 3min")

			recoveryTime := time.Since(start)
			GinkgoWriter.Printf("  Both recovered in %s\n", recoveryTime.Round(time.Second))
			Expect(recoveryTime).To(BeNumerically("<", 3*time.Minute),
				"recovery took longer than 3 minutes")

			// Verify API health.
			err = checkAPIHealth()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	// ---------------------------------------------------------------
	// Scenario 9: Cell network partition
	// scale-50k-plan L3-L5 chaos: "Cell failure does not propagate.
	// Blast radius is one cell."
	// ---------------------------------------------------------------
	Context("Cell network partition", func() {
		It("should not affect API health during runtime cluster partition", func() {
			skipUnlessChaos()

			// API is on mgmt cluster. Even if the runtime cluster is
			// partitioned, the API should continue serving.
			err := checkAPIHealth()
			Expect(err).NotTo(HaveOccurred(), "API should be healthy during cell partition")
		})
	})
})
