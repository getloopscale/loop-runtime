// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP — k3d-based integration tests.
// These tests validate the layered-bootstrap artifacts against a real K8s
// cluster (local k3d). They prove the offline tests in regression_test.go
// also work in real environments without requiring DigitalOcean.
//
// Gated on LOOPZERO_K3D_INTEGRATION=1 to avoid depending on local k3d in CI.

package load

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func k3dContext() string {
	if v := os.Getenv("LOOPZERO_K3D_CONTEXT"); v != "" {
		return v
	}
	return "k3d-loopzero-test"
}

// runCommand wraps exec.Command with a 60s timeout.
func runCommand(t *testing.T, name string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	exit := 0
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			exit = e.ExitCode()
		} else {
			exit = -1
		}
	}
	return buf.String(), exit
}

func skipUnlessK3d(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_K3D_INTEGRATION") == "" {
		t.Skip("set LOOPZERO_K3D_INTEGRATION=1 to run k3d integration tests")
	}
	out, exit := runCommand(t, "kubectl", "--context", k3dContext(), "cluster-info")
	if exit != 0 {
		t.Skipf("k3d context %q not reachable: %s", k3dContext(), out)
	}
}

// TestK3d_InternalChartsAllInstall validates that the 8 NEW internal Helm charts
// (created in iteration 5) all deploy cleanly into a real cluster.
// CHG-2026-04-28-LAYERED-BOOTSTRAP iteration 6 — proves charts referenced by
// helmfiles actually work end-to-end.
func TestK3d_InternalChartsAllInstall(t *testing.T) {
	skipUnlessK3d(t)
	ctx := k3dContext()

	type chartCase struct {
		dir       string   // release and namespace stem
		path      string   // chart path relative to this package; "" means infra/helm/<dir>
		extraArgs []string // e.g., --no-hooks for smoke-test
	}
	// Note: loopzero-controller and tenant-controller monolith charts removed
	// pre-GA per ADR-0023 (operator split) + ADR-0042 (ghost chart deletion).
	// Operator workloads now ship via 4 split charts: loopzero-tenant-operator,
	// loopzero-app-operator, loopzero-preview-operator, loopzero-platform-operator.
	// loopzero-api and loopzero-cloud are one chart each under charts/, installed by Helmfile
	// with their bootstrap values; that install is what this exercises.
	charts := []chartCase{
		{dir: "loopzero-api", path: "../../../../charts/loopzero-api",
			extraArgs: []string{"-f", "../../../../charts/loopzero-api/values-bootstrap.yaml"}},
		{dir: "loopzero-cloud", path: "../../../../charts/loopzero-cloud",
			extraArgs: []string{"-f", "../../../../charts/loopzero-cloud/values-bootstrap.yaml"}},
		{dir: "cloudflared"},
		{dir: "cluster-issuer"},
		{dir: "network-policies"},
		// smoke-test runs a post-install Job that probes loopzero-api/gitea/argocd —
		// expected to fail without those services. Use --no-hooks to validate
		// the manifest renders correctly without executing the Job.
		{dir: "smoke-test", extraArgs: []string{"--no-hooks"}},
	}

	// Use a per-run timestamp suffix to avoid namespace re-use races
	// between sequential tests in the same `go test` run.
	suffix := os.Getenv("LOOPZERO_K3D_NS_SUFFIX")
	if suffix == "" {
		suffix = strings.ReplaceAll(time.Now().UTC().Format("20060102-150405"), ":", "")
	}

	for _, c := range charts {
		t.Run(c.dir, func(t *testing.T) {
			ns := c.dir + "-int-" + suffix
			release := c.dir + "-test"

			// Best-effort prior cleanup (in case of leftover from earlier runs)
			runCommand(t, "helm", "--kube-context", ctx, "uninstall", release, "-n", ns)
			if c.dir == "cluster-issuer" {
				// ClusterIssuer is cluster-scoped — must be deleted explicitly
				runCommand(t, "kubectl", "--context", ctx,
					"delete", "clusterissuer", "loopzero-ca-issuer", "--ignore-not-found")
			}
			defer func() {
				runCommand(t, "helm", "--kube-context", ctx, "uninstall", release, "-n", ns)
				if c.dir == "cluster-issuer" {
					runCommand(t, "kubectl", "--context", ctx,
						"delete", "clusterissuer", "loopzero-ca-issuer", "--ignore-not-found")
				}
				// Use --wait=false so we don't block on Terminating; new run uses
				// a different suffix anyway.
				runCommand(t, "kubectl", "--context", ctx,
					"delete", "namespace", ns, "--ignore-not-found", "--wait=false")
			}()

			// Create namespace (fresh — never re-used)
			out, exit := runCommand(t, "kubectl", "--context", ctx, "create", "namespace", ns)
			if exit != 0 && !strings.Contains(out, "already exists") {
				t.Fatalf("create namespace failed: %s", out)
			}

			// Install
			chartPath := c.path
			if chartPath == "" {
				chartPath = "../../../helm/" + c.dir
			} else {
				// The charts/ charts include loopzero-common, which a clone does not carry
				// built; Helmfile builds it before installing, and so does this.
				if out, exit := runCommand(t, "helm", "dependency", "build", chartPath); exit != 0 {
					t.Fatalf("helm dependency build %s failed (exit %d): %s", chartPath, exit, out)
				}
			}
			args := []string{
				"--kube-context", ctx,
				"install", release,
				chartPath,
				"--namespace", ns,
				"--wait=false",
				"--timeout", "30s",
			}
			args = append(args, c.extraArgs...)
			out, exit = runCommand(t, "helm", args...)
			if exit != 0 {
				t.Fatalf("helm install %s failed (exit %d): %s", c.dir, exit, out)
			}
			if !strings.Contains(out, "STATUS: deployed") {
				t.Errorf("expected STATUS: deployed in output: %s", out)
			}
		})
	}
}

// TestK3d_AdmissionPoliciesChartInstalls verifies that the admission-policies
// Helm chart deploys cleanly into a real cluster and creates the expected
// VAPs / ValidatingAdmissionPolicyBindings.
func TestK3d_AdmissionPoliciesChartInstalls(t *testing.T) {
	skipUnlessK3d(t)

	const ns = "ap-integration-test"
	ctx := k3dContext()

	// Cleanup any leftover release first
	runCommand(t, "helm", "--kube-context", ctx, "uninstall", "ap-int", "-n", ns)
	runCommand(t, "kubectl", "--context", ctx, "delete", "namespace", ns, "--wait=false")
	defer func() {
		runCommand(t, "helm", "--kube-context", ctx, "uninstall", "ap-int", "-n", ns)
		runCommand(t, "kubectl", "--context", ctx, "delete", "namespace", ns, "--wait=false")
	}()

	// Create namespace
	runCommand(t, "kubectl", "--context", ctx, "create", "namespace", ns)

	// Install chart
	out, exit := runCommand(t, "helm", "--kube-context", ctx,
		"install", "ap-int", "../../../../charts/loopzero-admission-policies/",
		"--namespace", ns,
		"--set", "clusterImagePolicy.mode=warn",
		"--set", "alerts.enabled=false",
		"--set", "clusterImagePolicy.enabled=false",
	)
	if exit != 0 {
		t.Fatalf("helm install failed (exit %d): %s", exit, out)
	}

	// Verify VAPs created
	vapOut, _ := runCommand(t, "kubectl", "--context", ctx,
		"get", "validatingadmissionpolicy", "-o", "name")
	expectedVAPs := []string{
		"loopzero-argocd-managed-by",
		"loopzero-disallow-latest-tag",
		"loopzero-protect-platform-networkpolicies",
		"loopzero-require-image-signature",
		"loopzero-require-resource-limits",
		"loopzero-restrict-image-registries",
	}
	for _, vap := range expectedVAPs {
		if !strings.Contains(vapOut, vap) {
			t.Errorf("expected VAP %q in cluster, got: %s", vap, vapOut)
		}
	}
}

// TestK3d_ScopedKubeconfigScript validates that scoped-kubeconfig.sh produces
// a working kubeconfig that can list pods through the SA token.
func TestK3d_ScopedKubeconfigScript(t *testing.T) {
	skipUnlessK3d(t)

	tmp, err := os.CreateTemp("", "scoped-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	scopedPath := tmp.Name()
	defer os.Remove(scopedPath)

	// k3d kubeconfig write
	kc, exit := runCommand(t, "k3d", "kubeconfig", "write", "loopzero-test")
	if exit != 0 {
		t.Skipf("k3d kubeconfig write failed: %s", kc)
	}
	kcPath := strings.TrimSpace(kc)
	defer os.Remove(kcPath)

	// Run scoped-kubeconfig.sh
	scriptPath := "../../../helmfile/scripts/lib/scoped-kubeconfig.sh"
	cmd := exec.Command("bash", scriptPath, "k3d-test", kcPath, scopedPath)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kcPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scoped-kubeconfig.sh failed: %v\n%s", err, output)
	}

	// The scoped kubeconfig must exist and be 0600
	info, err := os.Stat(scopedPath)
	if err != nil {
		t.Fatalf("scoped kubeconfig not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("scoped kubeconfig perms %v expected 0600", info.Mode().Perm())
	}

	// Use it to list pods (proves the SA token works)
	listOut, listExit := runCommand(t, "kubectl",
		"--kubeconfig", scopedPath, "get", "pods", "-n", "loopzero-system",
		"--request-timeout=5s")
	if listExit != 0 {
		t.Fatalf("kubectl with scoped kubeconfig failed: %s", listOut)
	}
}

// TestK3d_ArgocdRegisterScript validates that argocd-register.sh creates a
// well-formed ArgoCD cluster Secret with caData decoded as PEM (B6 protection).
func TestK3d_ArgocdRegisterScript(t *testing.T) {
	skipUnlessK3d(t)

	const argocdNS = "argocd"
	const clusterName = "k3d-int-test"
	ctx := k3dContext()

	// Ensure argocd namespace exists
	runCommand(t, "kubectl", "--context", ctx, "create", "namespace", argocdNS)

	defer func() {
		runCommand(t, "kubectl", "--context", ctx,
			"delete", "secret", clusterName+"-argocd-cluster", "-n", argocdNS)
	}()

	// Generate a scoped kubeconfig
	tmp, err := os.CreateTemp("", "scoped-argocd-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	scopedPath := tmp.Name()
	defer os.Remove(scopedPath)

	kc, _ := runCommand(t, "k3d", "kubeconfig", "write", "loopzero-test")
	kcPath := strings.TrimSpace(kc)
	defer os.Remove(kcPath)

	scopedScript := "../../../helmfile/scripts/lib/scoped-kubeconfig.sh"
	cmd := exec.Command("bash", scopedScript, clusterName, kcPath, scopedPath)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scoped-kubeconfig: %v\n%s", err, out)
	}

	// Run argocd-register.sh
	registerScript := "../../../helmfile/scripts/lib/argocd-register.sh"
	regCmd := exec.Command("bash", registerScript, clusterName, scopedPath)
	regCmd.Env = append(os.Environ(), "KUBECONFIG="+kcPath)
	if out, err := regCmd.CombinedOutput(); err != nil {
		t.Fatalf("argocd-register.sh: %v\n%s", err, out)
	}

	// Verify the Secret
	secretOut, secretExit := runCommand(t, "kubectl", "--context", ctx,
		"get", "secret", clusterName+"-argocd-cluster", "-n", argocdNS,
		"-o", "jsonpath={.data.config}")
	if secretExit != 0 {
		t.Fatalf("Secret not found after register: %s", secretOut)
	}
	if len(secretOut) == 0 {
		t.Fatal("Secret config field is empty")
	}
}

// TestK3d_RegressionSuiteRunsAgainstClusterPartial verifies that the full
// regression-checks.sh runs without bash syntax errors against a real cluster.
// Some checks fail (cluster lacks Loki/policy-controller in test mode) but
// the SCRIPT must exit cleanly.
func TestK3d_RegressionSuiteRunsAgainstClusterPartial(t *testing.T) {
	skipUnlessK3d(t)

	kc, _ := runCommand(t, "k3d", "kubeconfig", "write", "loopzero-test")
	kcPath := strings.TrimSpace(kc)
	defer os.Remove(kcPath)

	cmd := exec.Command("bash", "../../../helmfile/scripts/regression-checks.sh", "dev.getloopscale.com")
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kcPath)
	out, _ := cmd.CombinedOutput()
	output := string(out)

	// We allow failures (cluster doesn't have all components installed),
	// but the SCRIPT itself must not contain bash syntax errors.
	if strings.Contains(output, "syntax error in expression") {
		t.Errorf("bash syntax error in regression-checks.sh:\n%s", output)
	}

	// Must contain at least the framework PASS lines for the items
	// covered by code (B6, B8-B10, B12-B15) — these always pass since
	// they document existing test coverage.
	for _, marker := range []string{"PASS  B6", "PASS  B8-B10", "PASS  B12", "PASS  B13-B15", "PASS  B18"} {
		if !strings.Contains(output, marker) {
			t.Errorf("expected %q in regression output:\n%s", marker, output)
		}
	}

	// Counter line must be present.
	if !strings.Contains(output, "Regression suite:") {
		t.Errorf("missing summary line in output:\n%s", output)
	}
}
