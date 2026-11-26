// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Ginkgo migration of infra/tests/dogfood/repro-findings.sh (M18 T2.1 Wave B).
//
// Reproduces the 13 canonical dogfood 2026-04-29 findings as Ginkgo specs.
// Also folds in the cleanup helper (was cleanup-terminating-namespaces.sh).
//
// Hermetic mode (LOOP_RUNTIME_E2E=""): all specs Skip() gracefully — compiles and
// passes in CI without cluster. LIVE mode (LOOP_RUNTIME_E2E=1): each finding is
// probed against dev-public k3d clusters via kubectl/curl/k3d subprocesses.
//
// This is the M18 hermetic slice per plan v1.0 ADR-D1 + blueprint SHIPPABLE 99.7.
// The 2 bash scripts (repro-findings.sh + cleanup-terminating-namespaces.sh)
// are deleted in the same commit.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// runKubectl runs kubectl against the given kubeconfig with the given args.
// Returns (stdout, exitCode, error).
func runKubectl(ctx context.Context, kubeconfig string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if kubeconfig != "" {
		cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	}
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
			err = nil
		}
	}
	return string(out), exitCode, err
}

// runCurl probes a URL with a max timeout; returns the HTTP status code as a string.
func runCurl(ctx context.Context, url string, timeoutSec int) (string, error) {
	cmd := exec.CommandContext(ctx, "curl", "-s", "-o", "/dev/null",
		"-w", "%{http_code}", "--max-time", fmt.Sprintf("%d", timeoutSec), url)
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// curl exit non-zero is data, not error
			return "000", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// checkPodPhase returns the .status.phase of a specific pod (or "" if missing).
func checkPodPhase(ctx context.Context, kubeconfig, namespace, pod string) string {
	out, _, err := runKubectl(ctx, kubeconfig, "-n", namespace, "get", "pod", pod,
		"-o", "jsonpath={.status.phase}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// cleanupTerminatingNamespaces removes finalizers from stuck loop-runtime-test-*
// namespaces (folded from cleanup-terminating-namespaces.sh).
// Safe: ONLY touches loop-runtime-test-*, build-test-*, build-vap-test-*, loop-runtime-vap-test-*
// prefixed namespaces. Production tenant namespaces (loop-runtime-{tenant}-*) are NEVER
// touched — safelist enforced explicitly.
func cleanupTerminatingNamespaces(ctx context.Context, kubeconfig string) (int, error) {
	safePrefixes := []string{
		"loop-runtime-test-", "build-test-", "build-vap-test-", "loop-runtime-vap-test-",
	}
	out, _, err := runKubectl(ctx, kubeconfig, "get", "ns",
		"-o", "jsonpath={range .items[?(@.status.phase=='Terminating')]}{.metadata.name}{'\\n'}{end}")
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, ns := range strings.Split(strings.TrimSpace(out), "\n") {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		safe := false
		for _, prefix := range safePrefixes {
			if strings.HasPrefix(ns, prefix) {
				safe = true
				break
			}
		}
		if !safe {
			// SKIP: not in safelist. Never force-finalize production tenant ns.
			continue
		}
		// Patch finalizers empty; kube-controller then removes namespace.
		patchArgs := []string{"patch", "ns", ns, "--type=merge",
			"-p", `{"metadata":{"finalizers":null}}`}
		if _, exitCode, err := runKubectl(ctx, kubeconfig, patchArgs...); err == nil && exitCode == 0 {
			cleaned++
		}
	}
	return cleaned, nil
}

// hermeticSkipIfNoLive is called at each spec top: skips if LOOP_RUNTIME_E2E is unset.
func hermeticSkipIfNoLive() {
	if os.Getenv("LOOP_RUNTIME_E2E") == "" {
		Skip("hermetic mode — Set LOOP_RUNTIME_E2E=1 for LIVE dogfood repro")
	}
}

var _ = Describe("Dogfood 2026-04-29 findings repro", Label("dogfood-repro", "live"), func() {

	var (
		mgmtKube     string
		workloadKube string
	)

	BeforeEach(func() {
		hermeticSkipIfNoLive()

		// Live mode: resolve kubeconfigs via k3d.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "k3d", "kubeconfig", "write", "loop-runtime-core").Output()
		if err != nil {
			Skip("k3d loop-runtime-core not available: " + err.Error())
		}
		mgmtKube = strings.TrimSpace(string(out))
		out, err = exec.CommandContext(ctx, "k3d", "kubeconfig", "write", "loop-runtime-cell-0").Output()
		if err != nil {
			Skip("k3d loop-runtime-cell-0 not available: " + err.Error())
		}
		workloadKube = strings.TrimSpace(string(out))
		_ = mgmtKube
		_ = workloadKube
	})

	// The 13 canonical dogfood 2026-04-29 findings. Each spec probes the
	// symptom that flagged the finding originally. FIXED = observed absent;
	// REPRODUCES = observed present. Ginkgo `It` semantics: pass = "still bad"
	// if we designed the assertion to allow reproduction, or pass = "fixed"
	// if we designed the assertion for the fix. Below we assert the FIXED
	// state; failure = regression.

	It("F-01: API health endpoint reachable at localhost:13001", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		code, err := runCurl(ctx, "http://localhost:13001/api/v1/health", 3)
		Expect(err).NotTo(HaveOccurred())
		Expect(code).To(Equal("200"), "API not reachable at localhost:13001 (regression F-01)")
	})

	It("F-02: loop-runtime-api pod Running in loop-runtime-system namespace", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _, err := runKubectl(ctx, mgmtKube, "-n", "loop-runtime-system", "get", "pods",
			"-l", "app.kubernetes.io/name=loop-runtime-api",
			"-o", "jsonpath={.items[*].status.phase}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("Running"), "loop-runtime-api not Running (regression F-02)")
	})

	It("F-03: Kratos courier StatefulSet available", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _, err := runKubectl(ctx, mgmtKube, "-n", "loop-runtime-auth", "get", "statefulset",
			"ory-kratos-courier",
			"-o", "jsonpath={.status.readyReplicas}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("1"), "kratos courier not ready (regression F-03)")
	})

	It("F-04: schema_migrations table populated", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, _, err := runKubectl(ctx, mgmtKube, "-n", "loop-runtime-system", "exec", "postgresql-0",
			"--", "psql", "-U", "loop_runtime", "-t", "-c", "SELECT count(*) FROM schema_migrations")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(Equal("0"), "schema_migrations empty (regression F-04)")
	})

	It("F-05: ClusterCredentials for cell-0 connected", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _, err := runKubectl(ctx, mgmtKube, "-n", "loop-runtime-system", "get", "clustercredentials",
			"cell-0", "-o", "jsonpath={.status.state}")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).To(Equal("Ready"), "cell-0 not Ready (regression F-05)")
	})

	It("F-06: loop-console dashboard reachable at localhost:9090", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		code, err := runCurl(ctx, "http://localhost:9090/health", 3)
		Expect(err).NotTo(HaveOccurred())
		Expect(code).To(Or(Equal("200"), Equal("404")), "loop-console unreachable (regression F-06)")
	})

	It("F-07: MinIO Ready", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		phase := checkPodPhase(ctx, mgmtKube, "loop-runtime-system", "minio-0")
		Expect(phase).To(Equal("Running"), "MinIO not Running (regression F-07)")
	})

	It("F-08: ESO SecretStore not stuck 401 (loop-runtime-cluster-kubeconfigs)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _, err := runKubectl(ctx, mgmtKube, "-n", "loop-runtime-system", "get", "secretstore",
			"loop-runtime-cluster-kubeconfigs", "-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).To(Equal("True"), "ESO SecretStore stale (regression F-08)")
	})

	It("F-09: loop-runtime-api canonical JSON responses (no HTML in --json paths)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "curl", "-s", "-H", "Accept: application/json",
			"http://localhost:13001/api/v1/projects")
		out, _ := cmd.Output()
		var v any
		err := json.Unmarshal(out, &v)
		Expect(err).NotTo(HaveOccurred(), "loop-runtime-api returned non-JSON (regression F-09)")
	})

	It("F-10: Gitea root repos endpoint reachable", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		code, err := runCurl(ctx, "http://localhost:3000/", 3)
		Expect(err).NotTo(HaveOccurred())
		Expect(code).To(Or(Equal("200"), Equal("302")), "Gitea unreachable (regression F-10)")
	})

	It("F-11: ArgoCD Applications not stuck Unknown", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _, err := runKubectl(ctx, mgmtKube, "-n", "argocd", "get", "applications.argoproj.io",
			"-o", "jsonpath={.items[?(@.status.health.status=='Unknown')].metadata.name}")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).To(BeEmpty(), "ArgoCD apps stuck Unknown (regression F-11)")
	})

	It("F-12: loop-runtime-core cluster reachable via k3d kubeconfig", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, exitCode, err := runKubectl(ctx, mgmtKube, "cluster-info")
		Expect(err).NotTo(HaveOccurred())
		Expect(exitCode).To(Equal(0), "kubectl cluster-info failed (regression F-12)")
		Expect(out).To(ContainSubstring("running"), "cluster not running (regression F-12)")
	})

	It("F-13: NetworkPolicies present for tenant isolation", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _, err := runKubectl(ctx, workloadKube, "get", "networkpolicies", "-A",
			"-o", "jsonpath={.items[*].metadata.name}")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty(), "no NetworkPolicies present (regression F-13)")
	})
})

var _ = Describe("Dogfood cleanup helper (folded from cleanup-terminating-namespaces.sh)",
	Label("dogfood-cleanup", "live"), func() {

		It("cleans up loop-runtime-test-* Terminating namespaces safely", func() {
			hermeticSkipIfNoLive()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			out, err := exec.CommandContext(ctx, "k3d", "kubeconfig", "write", "loop-runtime-cell-0").Output()
			if err != nil {
				Skip("k3d loop-runtime-cell-0 not available")
			}
			workloadKube := strings.TrimSpace(string(out))

			cleaned, err := cleanupTerminatingNamespaces(ctx, workloadKube)
			Expect(err).NotTo(HaveOccurred())
			// Assertion: helper ran without panic + safelist enforced.
			// cleaned may be 0 (no stuck ns) or positive; both are acceptable.
			_ = cleaned
		})
	})
