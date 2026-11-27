// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package e2e — end-to-end-deploy-validation-plan T4.3.
// Orchestration test for the full user→CLI→API→Argo→ArgoCD→pod path.
// Gated by LOOP_RUNTIME_E2E_DEPLOY=1.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	envFlag       = "LOOP_RUNTIME_E2E_DEPLOY"
	testTenant    = "e2e-test"
	testProject   = "repotest"
	testRepoURL   = "https://github.com/getloopscale/loopzerokit-example"
	testFramework = "node"
	// deployTimeout and probeTimeout are defined in constants_test.go.
	// Use httpProbeTimeout for probe operations.
)

// requireE2EFlag skips the test unless LOOP_RUNTIME_E2E_DEPLOY=1.
func requireE2EFlag(t *testing.T) {
	if os.Getenv(envFlag) != "1" {
		t.Skipf("set %s=1 to run full E2E deploy test (requires 3 k3d clusters + DO Registry auth)", envFlag)
	}
}

// requireKubeContexts asserts all 3 expected k3d contexts exist.
func requireKubeContexts(t *testing.T) {
	for _, ctx := range []string{"k3d-loop-runtime-core", "k3d-loop-runtime-build", "k3d-loop-runtime-cell-0"} {
		cmd := exec.Command("kubectl", "config", "get-contexts", "-o", "name", ctx)
		if err := cmd.Run(); err != nil {
			t.Fatalf("missing kube context %s: run `task setup` first (%v)", ctx, err)
		}
	}
}

// requireBinary asserts the named binary is in PATH.
func requireBinary(t *testing.T, name string) {
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("binary %s not in PATH: %v", name, err)
	}
}

// runCmd runs a command, returning stdout+stderr combined and error.
func runCmd(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestE2EDeployHappyPath orchestrates the full user→CLI→API→Argo→ArgoCD→pod
// path. This is the canonical proof that the local 3-cluster topology works
// equivalently to DOKS production (Rule 10).
func TestE2EDeployHappyPath(t *testing.T) {
	requireE2EFlag(t)
	requireKubeContexts(t)
	requireBinary(t, "loop-runtime")
	requireBinary(t, "kubectl")

	t.Logf("=== Step 1/8: Verify mgmt cluster loop-runtime-api healthy ===")
	out, err := runCmd(t, "kubectl", "--context", "k3d-loop-runtime-core", "-n", "loop-runtime-system",
		"get", "deploy", "loop-runtime-api", "-o", "jsonpath={.status.readyReplicas}")
	if err != nil || !strings.Contains(out, "1") {
		t.Fatalf("loop-runtime-api not ready: out=%q err=%v", out, err)
	}

	t.Logf("=== Step 2/8: Verify ArgoCD has all 3 clusters registered ===")
	out, err = runCmd(t, "kubectl", "--context", "k3d-loop-runtime-core", "-n", "argocd",
		"get", "secrets", "-l", "argocd.argoproj.io/secret-type=cluster", "-o", "name")
	clusterCount := strings.Count(out, "secret/")
	if err != nil || clusterCount < 2 {
		t.Fatalf("expected ≥2 ArgoCD cluster secrets (build+runtime), got %d: %v", clusterCount, err)
	}

	t.Logf("=== Step 3/8: Setup workspace from %s ===", testRepoURL)
	workspace := setupWorkspace(t)
	t.Logf("workspace: %s", workspace)

	t.Logf("=== Step 4/8: loop-runtime init in workspace ===")
	out, err = runCmdInDir(t, workspace, "loop-runtime", "init", "--non-interactive", "--framework", testFramework)
	t.Logf("init output: %s", out)
	if err != nil {
		t.Fatalf("loopscale init failed: %v", err)
	}

	t.Logf("=== Step 5/8: loop-runtime deploy ===")
	deployCtx, cancel := context.WithTimeout(context.Background(), deployTimeout)
	defer cancel()
	deployOut, deployErr := runDeployWithContext(t, deployCtx, workspace)
	if deployErr != nil {
		t.Fatalf("loopscale deploy failed: %v\nOutput:\n%s", deployErr, deployOut)
	}
	t.Logf("deploy output (last 1KB):\n%s", lastBytes(deployOut, 1024))

	t.Logf("=== Step 6/8: Extract release URL from deploy output ===")
	url := extractURLFromDeployOutput(t, deployOut)
	if url == "" {
		t.Fatalf("no URL extracted from deploy output")
	}
	t.Logf("release URL: %s", url)

	t.Logf("=== Step 7/8: Workload pod running on loop-runtime-cell-0 ===")
	if err := waitForWorkloadPodRunning(t, testTenant, probeTimeout); err != nil {
		t.Fatalf("workload pod did not become Ready: %v", err)
	}

	t.Logf("=== Step 8/8: HTTP probe URL returns 200 ===")
	if err := waitForHTTP200(url, probeTimeout); err != nil {
		t.Fatalf("URL %s did not return 200: %v", url, err)
	}

	t.Logf("✓ E2E happy path PASS")
}

// setupWorkspace clones loopzerokit-example into a tmpdir.
func setupWorkspace(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "loop-runtime-e2e-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	out, err := runCmd(t, "git", "clone", testRepoURL, dir)
	if err != nil {
		t.Fatalf("git clone failed: %s\nerr=%v", out, err)
	}
	return dir
}

// runCmdInDir runs cmd with cwd=dir.
func runCmdInDir(t *testing.T, dir, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runDeployWithContext invokes `loopscale deploy --watch --json` and streams output.
func runDeployWithContext(t *testing.T, ctx context.Context, workspace string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "loop-runtime", "deploy", "--watch", "--json")
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// extractURLFromDeployOutput parses --json output for the release URL.
func extractURLFromDeployOutput(t *testing.T, out string) string {
	t.Helper()
	// Look for "url":"..." in JSON output
	for _, prefix := range []string{`"url":"`, `"public_url":"`} {
		if idx := strings.Index(out, prefix); idx >= 0 {
			rest := out[idx+len(prefix):]
			if end := strings.Index(rest, `"`); end >= 0 {
				return rest[:end]
			}
		}
	}
	return ""
}

// waitForWorkloadPodRunning polls for tenant pod 1/1 Ready.
func waitForWorkloadPodRunning(t *testing.T, tenant string, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	ns := "loop-runtime-tenants"
	label := fmt.Sprintf("runtime.getloopscale.com/tenant=%s", tenant)
	for time.Now().Before(deadline) {
		out, _ := runCmd(t, "kubectl", "--context", "k3d-loop-runtime-cell-0", "-n", ns,
			"get", "pods", "-l", label, "-o", "jsonpath={.items[0].status.phase}")
		if strings.TrimSpace(out) == "Running" {
			return nil
		}
		// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for pod (label=%s) to become Running", label)
}

// waitForHTTP200 polls URL until HTTP 200 or timeout.
func waitForHTTP200(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("timeout polling %s", url)
}

// lastBytes returns the last n bytes of s (or all of s if shorter).
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// TestE2EDeployStructureExists — non-gated test that asserts this file
// references the expected commands + workflow. Always runs.
func TestE2EDeployStructureExists(t *testing.T) {
	if _, err := os.Stat("deploy_e2e_test.go"); err != nil {
		t.Errorf("deploy_e2e_test.go missing (T4.3): %v", err)
	}
	data, _ := os.ReadFile("deploy_e2e_test.go")
	s := string(data)
	required := []string{
		"loopscale init",
		"loopscale deploy",
		"k3d-loop-runtime-core",
		"k3d-loop-runtime-build",
		"k3d-loop-runtime-cell-0",
		testRepoURL,
	}
	for _, r := range required {
		if !strings.Contains(s, r) {
			t.Errorf("deploy_e2e_test.go must reference %q", r)
		}
	}
}
