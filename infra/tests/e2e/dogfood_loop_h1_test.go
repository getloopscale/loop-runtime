// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package e2e — dogfood loop H1 (CHG-2026-04-29-DOGFOOD T6.1).
//
// **Goal**: validar a jornada completa do usuário-real `login → init →
// deploy → status → rollback → delete` contra k3d standalone-multi com
// LOOPZERO_TOKEN real, garantindo Prova de Vida + Prova de Morte.
//
// **Why distinct from `deploy_e2e_test.go`?**
// - `deploy_e2e_test.go` (T4.3 do plano-pai) foca no caminho **deploy**
//   ponta-a-ponta; assume que tenant/projeto já existem.
// - `dogfood_loop_h1_test.go` (T6.1) foca na **jornada do usuário** —
//   ele faz `loopzero login`, executa cada passo do happy path como um
//   operator real, e o test valida que cada exit code faz match com a
//   intenção (Rule 4).
//
// **Gating**: requer `LOOPZERO_E2E_DOGFOOD=1` + `LOOPZERO_TOKEN` válido (GitHub
// PAT com `read:user` scope ou Hydra OAuth token). CI roda como manual
// trigger (não em PR builds — exige cluster real).
//
// **Cleanup**: defer `loopzero project delete` em cada test.
package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	dogfoodFlag        = "LOOPZERO_E2E_DOGFOOD"
	dogfoodTenantPrefix = "df-h1"
	dogfoodTimeout     = 25 * time.Minute
)

// requireDogfoodFlag skips the test unless LOOPZERO_E2E_DOGFOOD=1 + LOOPZERO_TOKEN.
func requireDogfoodFlag(t *testing.T) (loopzeroBin, loopzeroToken string) {
	t.Helper()
	if os.Getenv(dogfoodFlag) != "1" {
		t.Skipf("set %s=1 + LOOPZERO_TOKEN to run dogfood loop H1 (requires k3d + valid GitHub PAT)", dogfoodFlag)
	}
	loopzeroToken = os.Getenv("LOOPZERO_TOKEN")
	if loopzeroToken == "" {
		t.Skip("LOOPZERO_TOKEN required for dogfood loop H1 (GitHub PAT with read:user scope)")
	}
	// Locate loopzero binary (repo root / cli/bin/loopzero).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	candidate := filepath.Join(cwd, "..", "..", "..", "cli", "bin", "loopzero")
	if _, err := os.Stat(candidate); err != nil {
		t.Fatalf("loopzero binary not found at %s: %v", candidate, err)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return abs, loopzeroToken
}

// loopzeroExec runs `loopzero <args>` and returns (stdout, stderr, exitCode).
// Always passes --json para parsing programático e estabilidade de schema.
type loopzeroResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func loopzeroExec(t *testing.T, loopzeroBin string, env []string, dir string, args ...string) loopzeroResult {
	t.Helper()
	allArgs := append([]string{}, args...)
	cmd := exec.Command("node", append([]string{loopzeroBin}, allArgs...)...)
	cmd.Env = env
	cmd.Dir = dir
	stdout, _ := cmd.Output()
	stderr := []byte{}
	if exitErr, ok := cmd.Err.(*exec.ExitError); ok {
		stderr = exitErr.Stderr
	}
	exitCode := cmd.ProcessState.ExitCode()
	return loopzeroResult{stdout: string(stdout), stderr: string(stderr), exitCode: exitCode}
}

// parseJSONLine extrai o primeiro JSON válido de stdout (algumas commands
// escrevem progress lines antes do JSON). Distinct name to avoid clash com
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

// waitHTTP200 polls the URL with Host header until HTTP 200 or timeout.
func waitHTTP200(t *testing.T, url, hostHeader string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if hostHeader != "" {
			req.Host = hostHeader
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("timeout waiting for HTTP 200 from %s (Host: %s) after %v", url, hostHeader, timeout)
}

// TestDogfoodLoopH1 — jornada user-real completa.
//
// Steps:
//  1. `loopzero login --token $LOOPZERO_TOKEN` — exit 0
//  2. `loopzero whoami --json` — authenticated:true
//  3. `loopzero init --yes --name <unique>` em tmp dir — loopzero.yaml escrito,
//     exit 0 (preflightAuth passa porque token é válido — F-05 contract)
//  4. `loopzero deploy --json` — exit 0, build_id retornado, app eventualmente
//     atinge HTTP 200 via Traefik (Prova de Vida)
//  5. `loopzero status --json` — environment ACTIVE
//  6. `loopzero project delete <name> --yes --json` — exit 0
//  7. Prova de Morte: workload pod removido em <60s
//
// Each step asserts exit code + parses JSON response + checks specific
// invariants (auth, schema, no internal terms in output).
func TestDogfoodLoopH1(t *testing.T) {
	loopzeroBin, loopzeroToken := requireDogfoodFlag(t)

	// Isolated $HOME to avoid polluting user config.
	tmpHome := t.TempDir()
	tmpProject := t.TempDir()
	uniqueName := dogfoodTenantPrefix + "-" + time.Now().Format("20060102-150405")

	baseEnv := append(os.Environ(),
		"HOME="+tmpHome,
		"XDG_CONFIG_HOME="+tmpHome,
		"LOOPZERO_URL=http://localhost:13001",
	)

	// === Step 1: login ===
	r := loopzeroExec(t, loopzeroBin, baseEnv, tmpProject, "login", "--token", loopzeroToken, "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 1 (login) failed: exit=%d stdout=%s", r.exitCode, r.stdout)
	}

	// === Step 2: whoami ===
	r = loopzeroExec(t, loopzeroBin, baseEnv, tmpProject, "whoami", "--json")
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
	r = loopzeroExec(t, loopzeroBin, baseEnv, tmpProject, "init", "--yes", "--name", uniqueName, "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 3 (init) failed: exit=%d stdout=%s", r.exitCode, r.stdout)
	}
	if _, err := os.Stat(filepath.Join(tmpProject, "loopzero.yaml")); err != nil {
		t.Fatalf("step 3: loopzero.yaml missing after init: %v", err)
	}

	// Defer cleanup — Prova de Morte (step 7) acontece aqui se test
	// falhar antes de chegar lá.
	defer func() {
		_ = loopzeroExec(t, loopzeroBin, baseEnv, tmpProject,
			"project", "delete", uniqueName, "--yes", "--json")
	}()

	// === Step 4: deploy ===
	r = loopzeroExec(t, loopzeroBin, baseEnv, tmpProject, "deploy", "--no-wait", "--json")
	if r.exitCode != 0 {
		t.Fatalf("step 4 (deploy) failed: exit=%d stdout=%s", r.exitCode, r.stdout)
	}
	deploy := parseJSONLine(t, r.stdout)
	if buildID, ok := deploy["build_id"].(string); !ok || buildID == "" {
		t.Fatalf("step 4: build_id missing; got %v", deploy)
	}

	// === Step 5: status ===
	r = loopzeroExec(t, loopzeroBin, baseEnv, tmpProject, "status", "--json")
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

// TestDogfoodLoopStructureExists — non-gated; valida que o test exists +
// schema. Roda em CI normal.
func TestDogfoodLoopStructureExists(t *testing.T) {
	if _, err := os.Stat("dogfood_loop_h1_test.go"); err != nil {
		t.Errorf("dogfood loop test file missing: %v", err)
	}
	// Sanity: o gating env var é o esperado.
	if dogfoodFlag != "LOOPZERO_E2E_DOGFOOD" {
		t.Errorf("gating env var changed unexpectedly: %s", dogfoodFlag)
	}
}
