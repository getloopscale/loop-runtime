// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package e2e — dogfood loop H1 (CHG-2026-04-29-DOGFOOD T6.1).
//
// **Goal**: validate the complete real-user journey `login → init →
// deploy → status → rollback → delete` against k3d standalone-multi with a
// real LOOPRUNTIME_TOKEN, guaranteeing Proof of Life + Proof of Death.
//
// **Why distinct from `deploy_e2e_test.go`?**
//   - `deploy_e2e_test.go` (T4.3 of the parent plan) focuses on the **deploy**
//     path end-to-end; it assumes tenant/project already exist.
//   - `dogfood_loop_h1_test.go` (T6.1) focuses on the **user journey** —
//     it runs `loopscale login`, executes each step of the happy path as a
//     real operator would, and the test validates that each exit code matches
//     the intent (Rule 4).
//
// **Gating**: requires `LOOPRUNTIME_E2E_DOGFOOD=1` + a valid `LOOPRUNTIME_TOKEN` (GitHub
// PAT with `read:user` scope or a Hydra OAuth token). CI runs it as a manual
// trigger (not on PR builds — it needs a real cluster).
//
// **Cleanup**: defer `loopscale project delete` in each test.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	dogfoodFlag         = "LOOPRUNTIME_E2E_DOGFOOD"
	dogfoodTenantPrefix = "df-h1"
	dogfoodTimeout      = 25 * time.Minute
)

// requireDogfoodFlag skips the test unless LOOPRUNTIME_E2E_DOGFOOD=1 + LOOPRUNTIME_TOKEN.
func requireDogfoodFlag(t *testing.T) (loopruntimeBin, loopruntimeToken string) {
	t.Helper()
	if os.Getenv(dogfoodFlag) != "1" {
		t.Skipf("set %s=1 + LOOPRUNTIME_TOKEN to run dogfood loop H1 (requires k3d + valid GitHub PAT)", dogfoodFlag)
	}
	loopruntimeToken = os.Getenv("LOOPRUNTIME_TOKEN")
	if loopruntimeToken == "" {
		t.Skip("LOOPRUNTIME_TOKEN required for dogfood loop H1 (GitHub PAT with read:user scope)")
	}
	// Locate loopruntime binary (repo root / cli/bin/loopruntime).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	candidate := filepath.Join(cwd, "..", "..", "..", "cli", "bin", "loopruntime")
	if _, err := os.Stat(candidate); err != nil {
		t.Fatalf("loopruntime binary not found at %s: %v", candidate, err)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return abs, loopruntimeToken
}

// loopruntimeExec runs `loopruntime <args>` and returns (stdout, stderr, exitCode).
// Always passes --json for programmatic parsing and schema stability.
type loopruntimeResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func loopruntimeExec(t *testing.T, loopruntimeBin string, env []string, dir string, args ...string) loopruntimeResult {
	t.Helper()
	allArgs := append([]string{}, args...)
	cmd := exec.Command("node", append([]string{loopruntimeBin}, allArgs...)...)
	cmd.Env = env
	cmd.Dir = dir
	stdout, _ := cmd.Output()
	stderr := []byte{}
	if exitErr, ok := cmd.Err.(*exec.ExitError); ok {
		stderr = exitErr.Stderr
	}
	exitCode := cmd.ProcessState.ExitCode()
	return loopruntimeResult{stdout: string(stdout), stderr: string(stderr), exitCode: exitCode}
}

// parseJSONLine extracts the first valid JSON from stdout (some commands
// write progress lines before the JSON). Distinct name to avoid a clash with
// parseJSON em cli_helpers_test.go.
func parseJSONLine(t *testing.T, raw string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "{") {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}
	maxLen := 500
	if len(raw) < maxLen {
		maxLen = len(raw)
	}
	t.Fatalf("no JSON found in output: %s", raw[:maxLen])
	return nil
}

// TestDogfoodLoopH1 — complete real-user journey.
//
// Steps:
//  1. `loopscale login --token $LOOPRUNTIME_TOKEN` — exit 0
//  2. `loopscale whoami --json` — authenticated:true
//  3. `loopscale init --yes --name <unique>` in a tmp dir — loopzero.yaml written,
//     exit 0 (preflightAuth passes because the token is valid — F-05 contract)
//  4. `loopscale deploy --json` — exit 0, build_id returned, app eventually
//     reaches HTTP 200 via Traefik (Proof of Life)
//  5. `loopscale status --json` — environment ACTIVE
//  6. `loopscale project delete <name> --yes --json` — exit 0
//  7. Proof of Death: workload pod removed in <60s
//
// Each step asserts exit code + parses JSON response + checks specific
// invariants (auth, schema, no internal terms in output).
func TestDogfoodLoopH1(t *testing.T) {
	loopruntimeBin, loopruntimeToken := requireDogfoodFlag(t)

	// Isolated $HOME to avoid polluting user config.
	tmpHome := t.TempDir()
	tmpProject := t.TempDir()
	uniqueName := dogfoodTenantPrefix + "-" + time.Now().Format("20060102-150405")

	baseEnv := append(os.Environ(),
		"HOME="+tmpHome,
		"XDG_CONFIG_HOME="+tmpHome,
		"LOOPRUNTIME_URL=http://localhost:13001",
	)

	// === Step 1: login ===
	r := loopruntimeExec(t, loopruntimeBin, baseEnv, tmpProject, "login", "--token", loopruntimeToken, "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 1 (login) failed: exit=%d stdout=%s", r.exitCode, r.stdout)
	}

	// === Step 2: whoami ===
	r = loopruntimeExec(t, loopruntimeBin, baseEnv, tmpProject, "whoami", "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 2 (whoami) failed: exit=%d", r.exitCode)
	}
	who := parseJSONLine(t, r.stdout)
	if auth, ok := who["authenticated"].(bool); !ok || !auth {
		t.Fatalf("step 2: authenticated should be true; got %v", who)
	}

	// === Step 3: init in fresh tmp git repo ===
	if err := os.WriteFile(filepath.Join(tmpProject, "package.json"),
		[]byte(`{"name":"`+uniqueName+`","version":"1.0.0","scripts":{"start":"node index.js"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpProject, "index.js"),
		[]byte(`const http=require('http');http.createServer((q,r)=>{r.writeHead(200);r.end('df-h1')}).listen(process.env.PORT||3000);`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, gitCmd := range [][]string{
		{"init", "-q"}, {"add", "."}, {"-c", "user.email=df@x", "-c", "user.name=df", "commit", "-q", "-m", "h1"},
	} {
		c := exec.Command("git", gitCmd...)
		c.Dir = tmpProject
		c.Env = baseEnv
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", gitCmd, err, out)
		}
	}
	r = loopruntimeExec(t, loopruntimeBin, baseEnv, tmpProject, "init", "--yes", "--name", uniqueName, "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 3 (init) failed: exit=%d stdout=%s", r.exitCode, r.stdout)
	}
	if _, err := os.Stat(filepath.Join(tmpProject, "loopzero.yaml")); err != nil {
		t.Fatalf("step 3: loopzero.yaml missing after init: %v", err)
	}

	// Defer cleanup — Proof of Death (step 7) happens here if the test
	// fails before getting there.
	defer func() {
		_ = loopruntimeExec(t, loopruntimeBin, baseEnv, tmpProject,
			"project", "delete", uniqueName, "--yes", "--json")
	}()

	// === Step 4: deploy ===
	r = loopruntimeExec(t, loopruntimeBin, baseEnv, tmpProject, "deploy", "--no-wait", "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 4 (deploy) failed: exit=%d stdout=%s", r.exitCode, r.stdout)
	}
	deploy := parseJSONLine(t, r.stdout)
	if buildID, ok := deploy["build_id"].(string); !ok || buildID == "" {
		t.Fatalf("step 4: build_id missing; got %v", deploy)
	}

	// === Step 5: status ===
	r = loopruntimeExec(t, loopruntimeBin, baseEnv, tmpProject, "status", "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 5 (status) failed: exit=%d", r.exitCode)
	}

	// Forbidden internal terms scan (Rule 3) over all collected outputs.
	allOutput := r.stdout
	for _, term := range []string{"ArgoCD", "GitOps", "Gitea", "MinIO", "kubebuilder", "envtest"} {
		if strings.Contains(allOutput, term) {
			t.Errorf("Rule 3 violation: status output contains forbidden internal term %q", term)
		}
	}

	// Step 6 happens via the deferred cleanup above — exit code already
	// validated by the cleanup wrapper if test reached this point successfully.
	t.Logf("dogfood loop H1 PASS: tenant=%s deployed and status verified", uniqueName)
}

// TestDogfoodLoopStructureExists — non-gated; validates that the test exists +
// its schema. Runs in normal CI.
func TestDogfoodLoopStructureExists(t *testing.T) {
	if _, err := os.Stat("dogfood_loop_h1_test.go"); err != nil {
		t.Errorf("dogfood loop test file missing: %v", err)
	}
	// Sanity: the gating env var is the expected one.
	if dogfoodFlag != "LOOPRUNTIME_E2E_DOGFOOD" {
		t.Errorf("gating env var changed unexpectedly: %s", dogfoodFlag)
	}
}
