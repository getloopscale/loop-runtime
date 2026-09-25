// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package config — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOPZERO_REGRESSION=1 go test ./infra/tests/config/ -run TestRegression -v
package config

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	data, err := os.ReadFile("../../k3d/cell-0-cluster.yaml")
	if err != nil {
		t.Fatalf("read cell-0-cluster.yaml: %v", err)
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

// taskCmds returns the `cmds` of one task, with each entry as either the shell
// string it runs or the `task:` it delegates to. Two shapes, one accessor.
type taskCmds struct {
	Tasks map[string]struct {
		Cmds []yaml.Node `yaml:"cmds"`
	} `yaml:"tasks"`
}

func loadTaskCmds(t *testing.T, path, task string) []yaml.Node {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tf taskCmds
	if err := yaml.Unmarshal(data, &tf); err != nil {
		t.Fatalf("%s is not valid YAML: %v", path, err)
	}
	def, ok := tf.Tasks[task]
	if !ok {
		t.Fatalf("%s declares no task %q \u2014 re-point this test at the task that owns the "+
			"contract now; do NOT add a skip (B-182)", path, task)
	}
	if len(def.Cmds) == 0 {
		t.Fatalf("%s: task %q has no cmds", path, task)
	}
	return def.Cmds
}

// The `task setup` cluster-creation step is idempotent: re-running it does not
// destroy and recreate a cluster that is already there.
//
// B-182. This was TestBootstrapClusterScript, which read
// `infra/scripts/bootstrap-cluster.sh` and skipped when it was missing. That
// script was deleted deliberately in 346215cf0 (2026-05-23, "delete 3 standalone
// tools not wired into any automation"), and the commit message answers the exact
// question the old comment left open — "consolidated into task setup? test may be
// stale" — with "bootstrap-cluster.sh — superseded by `task setup` (k3d cluster
// create)". So the skip had been the only branch since that commit, and the
// idempotency assertion had been dead for three and a half months while the
// package printed `ok`.
//
// The CONTRACT outlived the script. `task setup` calls `clusters:create`, and every
// cluster there is created behind the same guard the deleted script used, in the
// same idiom:
//
//	k3d cluster list -o json | grep -q '"name":"loopzero-core"' || k3d cluster create ...
//
// That is what "idempotent" means here, and it is what this test asserts now, on
// the live file. It is a stronger check than the one it replaces: the old test
// proved the string "k3d cluster list" appeared SOMEWHERE in a script, so a second,
// unguarded create in the same file would have passed. This one requires every
// create in the task to carry its own guard, and it fails on the fifth cluster
// added without one.
//
// Scope is `clusters:create` alone, on purpose. `clusters:recreate` in the same
// file runs `k3d cluster delete` followed by an unguarded `k3d cluster create` —
// that is its whole reason to exist, it is not on the `task setup` path, and a
// check that swept the file would fail on correct code.
func TestClusterCreateIsIdempotent(t *testing.T) {
	const (
		rootTaskfile = "../../../Taskfile.yml"
		k3dTaskfile  = "../../../taskfiles/k3d.yml"
		createTask   = "clusters:create"
	)

	// `task setup` must still reach clusters:create, or the guard below protects a
	// task nobody runs — which is how the previous version of this test died.
	delegatesToCreate := false
	for _, cmd := range loadTaskCmds(t, rootTaskfile, "setup") {
		var delegate struct {
			Task string `yaml:"task"`
		}
		if cmd.Decode(&delegate) == nil && delegate.Task == createTask {
			delegatesToCreate = true
			break
		}
	}
	if !delegatesToCreate {
		t.Fatalf("%s: task `setup` no longer calls %q \u2014 whatever creates the clusters now is "+
			"what this idempotency test must follow", rootTaskfile, createTask)
	}

	creates := 0
	for _, cmd := range loadTaskCmds(t, k3dTaskfile, createTask) {
		var shell string
		if cmd.Decode(&shell) != nil || !strings.Contains(shell, "k3d cluster create") {
			continue
		}
		creates++
		if !strings.Contains(shell, "k3d cluster list") {
			t.Errorf("%s: task %q creates a cluster with no existence check, so a re-run of "+
				"`task setup` would not be idempotent:\n\t%s", k3dTaskfile, createTask, shell)
		}
	}
	if creates == 0 {
		t.Fatalf("%s: task %q creates no k3d cluster at all \u2014 this test is reading the wrong "+
			"task and is asserting nothing", k3dTaskfile, createTask)
	}
}

// T1.2 was REMOVED, not skipped (B-180, applying B-169's standard).
//
// It asserted that `cli/tests/integration/exit-code-contract.test.ts` exists and
// covers six commands plus --help/--version. That file moved to the sibling
// `loopzero-cli` repository in the M5 decomposition; `cli/` is not in this tree and
// `loopzero-cli` is not cloned beside it, so the stat always failed and the skip was
// the only branch ever taken. Everything below it -- the whole contract matrix --
// had not run since M5 while the package reported `ok`.
//
// It was also the weakest KIND of assertion: meta-coverage. It checked that
// another repository's test FILE exists and mentions some strings, which is a
// claim about a file listing rather than about behaviour. B-169 retired the same
// shape (`TestT04_InvariantsDocExists`) for the same reason: once each invariant
// has its own executable guard, an index that says the guards exist adds no
// signal and rots on its own schedule.
//
// What covers the exit-code contract now: loopzero-cli owns its own suite
// (`npm test -- tests/contract/engine-contract.test.ts`), invoked from this
// repository by `task quality:contract:cross-repo`, which prints an explicit SKIP
// when the sibling is absent instead of passing silently. The engine side of the
// same seam is `api/internal/contract/cli_consumer_test.go`, which is hermetic
// and runs here on every `task quality:contract`. The seam is tabulated in
// `docs/cross-repo-contract-matrix.md`.
//
// The class is now guarded: `loopzero-ops quality test-skip-absent-path` fails when a
// test under infra/tests/ skips on a path absent from this tree.

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
	// Cleanup: a deferred project delete is mandatory (Proof of Death).
	if !strings.Contains(s, `"project", "delete"`) {
		t.Error("T6.1 regression: test must defer project delete for cleanup (Proof of Death)")
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
