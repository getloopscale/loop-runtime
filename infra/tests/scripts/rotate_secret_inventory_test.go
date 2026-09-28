// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rotate-secret.sh resolves which ExternalSecrets to refresh from the chart
// inventory (infra/helm/loopzero-external-secrets/values.yaml), honouring each
// key's vaultBinaryOverride. The tenant operator's GITEA_TOKEN carries
// `vaultBinaryOverride: api`, so it reads api/secrets/gitea/token — the same
// path as the api's gitea-admin Secret. Rotating that path must refresh BOTH,
// or the operator keeps a revoked token. No entry reads
// operators/secrets/gitea/token (the eso-chart-sanity gate treats rendering it
// as a leak), so a rotation of that path has nothing to refresh and is refused.

func runRotateDryRun(t *testing.T, vaultPath string) (int, string) {
	t.Helper()
	if _, err := exec.LookPath("yq"); err != nil {
		t.Fatalf("yq is required to read the inventory: %v", err)
	}
	stub := t.TempDir()
	for _, tool := range []string{"bao", "kubectl"} {
		if err := os.WriteFile(filepath.Join(stub, tool), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "openbao", "rotate-secret.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "--env", "vault-local", "--path", vaultPath, "--dry-run")
	cmd.Env = append(os.Environ(), "PATH="+stub+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), string(out)
	default:
		t.Fatalf("run: %v", err)
		return -1, ""
	}
}

func infoLine(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func TestRotateSecret_RefreshesEveryExternalSecretThatReadsThePath(t *testing.T) {
	code, out := runRotateDryRun(t, "api/secrets/gitea/token")

	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	if got := infoLine(out, "[INFO] ExternalSecrets to force-refresh:"); got != "gitea-admin,loopzero-tenant-operator" {
		t.Errorf("ExternalSecrets = %q, want gitea-admin,loopzero-tenant-operator", got)
	}
	if got := infoLine(out, "[INFO] binaries to restart:"); got != "loopzero-api,loopzero-tenant-operator" {
		t.Errorf("binaries = %q, want loopzero-api,loopzero-tenant-operator", got)
	}
}

func TestRotateSecret_RefusesTheOperatorGiteaPathNoKeyReads(t *testing.T) {
	if code, out := runRotateDryRun(t, "operators/secrets/gitea/token"); code != 1 {
		t.Fatalf("exit = %d, want 1: every gitea token key overrides to api/secrets\n%s", code, out)
	}
}

func TestRotateSecret_RefusesAPathNoExternalSecretReads(t *testing.T) {
	if code, out := runRotateDryRun(t, "api/secrets/gitea/tokn"); code != 1 {
		t.Fatalf("exit = %d, want 1 for an unknown path\n%s", code, out)
	}
}
