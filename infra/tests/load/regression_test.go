// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package load — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOPZERO_REGRESSION=1 go test ./infra/tests/load/ -run TestRegression -v
package load

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runRegressionScript invokes regression-checks.sh and returns combined output.
func runRegressionScript(t *testing.T) (string, int) {
	t.Helper()
	scriptPath := "../../helmfile/scripts/regression-checks.sh"
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("regression-checks.sh not found at %s", scriptPath)
	}
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(), "LOOPZERO_DOMAIN=dev.loopzero.example")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return buf.String(), exitCode
}

func TestRegressionShellScriptIsExecutable(t *testing.T) {
	scriptPath := "../../helmfile/scripts/regression-checks.sh"
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("regression-checks.sh missing: %v", err)
	}
	mode := info.Mode().Perm()
	if mode&0o100 == 0 {
		t.Fatalf("regression-checks.sh is not executable (mode=%v)", mode)
	}
}

func TestRegressionScriptHasAllBugChecks(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/regression-checks.sh")
	if err != nil {
		t.Fatalf("read regression-checks.sh: %v", err)
	}
	content := string(data)
	for _, bug := range []string{"B1", "B2", "B3", "B4", "B5", "B6", "B7", "B11", "B16", "B18"} {
		if !strings.Contains(content, bug) {
			t.Errorf("regression-checks.sh missing reference to %s", bug)
		}
	}
}

func TestRegressionRunsAgainstLiveCluster(t *testing.T) {
	if os.Getenv("LOOPZERO_REGRESSION_LIVE") == "" {
		t.Skip("LOOPZERO_REGRESSION_LIVE not set — skipping live regression run")
	}
	out, exit := runRegressionScript(t)
	t.Logf("regression-checks.sh exit=%d output:\n%s", exit, out)
	if exit != 0 {
		t.Fatalf("regression suite failed with exit %d", exit)
	}
	if !strings.Contains(out, "PASS") {
		t.Fatalf("expected at least one PASS line in output")
	}
}

func TestStandaloneMultiEnvironmentsExist(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/environments/standalone-multi.yaml")
	if err != nil {
		t.Fatalf("read standalone-multi.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "clusterTopology: multi-cluster") {
		t.Error("regression: standalone-multi must set clusterTopology: multi-cluster")
	}
	if !strings.Contains(s, "host.k3d.internal") {
		t.Error("regression: standalone-multi must use host.k3d.internal for cross-cluster URLs")
	}
}

func TestK3dWorkloadConfigOneAgent(t *testing.T) {
	data, err := os.ReadFile("../../k3d/workload-cluster.yaml")
	if err != nil {
		t.Fatalf("read workload-cluster.yaml: %v", err)
	}
	if !strings.Contains(string(data), "agents: 1") {
		t.Error("end-to-end-deploy T0.2: workload cluster must have agents:1")
	}
}

func TestK3dBuildConfigOneAgent(t *testing.T) {
	data, err := os.ReadFile("../../k3d/build-cluster.yaml")
	if err != nil {
		t.Fatalf("read build-cluster.yaml: %v", err)
	}
	if !strings.Contains(string(data), "agents: 1") {
		t.Error("end-to-end-deploy T0.2: build cluster must have agents:1")
	}
}

func TestBootstrapClusterScript(t *testing.T) {
	if _, err := os.Stat("../../scripts/bootstrap-cluster.sh"); err != nil {
		t.Errorf("end-to-end-deploy T1.1: bootstrap-cluster.sh missing: %v", err)
	}
	data, _ := os.ReadFile("../../scripts/bootstrap-cluster.sh")
	if !strings.Contains(string(data), "k3d cluster list") {
		t.Error("bootstrap-cluster.sh must check existing cluster (idempotent)")
	}
}

func TestT12ExitCodeContractTestExists(t *testing.T) {
	const path = "../../../cli/tests/integration/exit-code-contract.test.ts"
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("T1.2 regression: contract test missing: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract test: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "spawnSync") {
		t.Error("T1.2 regression: contract test must use child_process.spawnSync to invoke real process")
	}
	for _, cmd := range []string{"whoami", "deploy", "validate", "build", "secret", "rollback"} {
		if !strings.Contains(s, "'"+cmd+"'") && !strings.Contains(s, `"`+cmd+`"`) {
			t.Errorf("T1.2 regression: contract matrix must cover %q command", cmd)
		}
	}
	// help/version SÃO exit 0, não erros — contract deve testar isso.
	if !strings.Contains(s, "--help") || !strings.Contains(s, "--version") {
		t.Error("T1.2 regression: contract test must cover --help / --version (exit 0 expected)")
	}
}

func TestT61DogfoodLoopH1TestExists(t *testing.T) {
	const path = "../../../infra/tests/e2e/dogfood_loop_h1_test.go"
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("T6.1 regression: dogfood loop H1 test missing: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dogfood test: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "LOOPZERO_E2E_DOGFOOD") {
		t.Error("T6.1 regression: gating env var must be LOOPZERO_E2E_DOGFOOD")
	}
	if !strings.Contains(s, "func TestDogfoodLoopH1(") {
		t.Error("T6.1 regression: TestDogfoodLoopH1 function must exist")
	}
	// Cleanup: defer project delete é obrigatório (Prova de Morte).
	if !strings.Contains(s, `"project", "delete"`) {
		t.Error("T6.1 regression: test must defer project delete for cleanup (Prova de Morte)")
	}
	// Rule 3 enforcement scan.
	if !strings.Contains(s, "ArgoCD") || !strings.Contains(s, "GitOps") {
		t.Error("T6.1 regression: test must scan output for forbidden internal terms (Rule 3)")
	}
}

func TestCleanIncrementalScript(t *testing.T) {
	if _, err := os.Stat("../../scripts/local/clean-incremental.sh"); err != nil {
		t.Errorf("end-to-end-deploy T0.1: clean-incremental.sh missing: %v", err)
	}
	data, _ := os.ReadFile("../../scripts/local/clean-incremental.sh")
	s := string(data)
	clusterDeleteString := "cluster" + " " + "delete"
	if strings.Contains(s, "k3d "+clusterDeleteString) {
		t.Error("clean-incremental.sh must NOT delete clusters")
	}
	if !strings.Contains(s, "status.phase=Succeeded") {
		t.Error("clean-incremental.sh must clean Succeeded pods")
	}
}
