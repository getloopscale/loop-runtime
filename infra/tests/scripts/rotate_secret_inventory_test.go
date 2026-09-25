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

// rotate-secret.sh decided which ExternalSecrets to refresh from its own
// hard-coded table, which had drifted from the chart inventory
// (infra/helm/loopzero-external-secrets/values.yaml) that seeds the secrets:
// rotating api/secrets/gitea/token also refreshed the operator's
// ExternalSecret, which reads operators/secrets/gitea/token and so never saw
// the new value, while operators/secrets/gitea/token itself was refused as an
// unknown path.

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

func TestRotateSecret_RefreshesOnlyTheExternalSecretsThatReadThePath(t *testing.T) {
	code, out := runRotateDryRun(t, "api/secrets/gitea/token")

	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	if got := infoLine(out, "[INFO] ExternalSecrets to force-refresh:"); got != "gitea-admin" {
		t.Errorf("ExternalSecrets = %q, want gitea-admin", got)
	}
	if got := infoLine(out, "[INFO] binaries to restart:"); got != "loopzero-api" {
		t.Errorf("binaries = %q, want loopzero-api", got)
	}
}

func TestRotateSecret_RotatesEveryPathTheInventorySeeds(t *testing.T) {
	code, out := runRotateDryRun(t, "operators/secrets/gitea/token")

	if code != 0 {
		t.Fatalf("exit = %d, want the operator's gitea token to be rotatable\n%s", code, out)
	}
	if got := infoLine(out, "[INFO] ExternalSecrets to force-refresh:"); got != "loopzero-tenant-operator" {
		t.Errorf("ExternalSecrets = %q, want loopzero-tenant-operator", got)
	}
	if got := infoLine(out, "[INFO] binaries to restart:"); got != "loopzero-tenant-operator" {
		t.Errorf("binaries = %q, want loopzero-tenant-operator", got)
	}
}

func TestRotateSecret_RefusesAPathNoExternalSecretReads(t *testing.T) {
	if code, out := runRotateDryRun(t, "api/secrets/gitea/tokn"); code != 1 {
		t.Fatalf("exit = %d, want 1 for an unknown path\n%s", code, out)
	}
}
