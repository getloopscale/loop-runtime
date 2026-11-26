// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// generate-secrets.sh --backup-only is the one Secret backup of the three bootstrap
// generators. The build and runtime generators used to write raw `kubectl get -o yaml`,
// which carries resourceVersion/uid and is refused on restore once the Secret changed.
// A fake kubectl on PATH stands in for the cluster.

const genSecrets = "../../helmfile/scripts/generate-secrets.sh"

const fakeSecretManifest = `apiVersion: v1
kind: Secret
metadata:
  name: buildkit-registry-config
  namespace: argo
  resourceVersion: "4711"
  uid: 0f0f0f0f-0000-0000-0000-000000000000
  creationTimestamp: "2026-01-01T00:00:00Z"
data:
  token: ZmFrZQ==
`

func fakeKubectl(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.yaml")
	if err := os.WriteFile(fixture, []byte(fakeSecretManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	// Serves the fixture for the Secret it knows and fails for any other, as kubectl does.
	script := "#!/usr/bin/env bash\ncase \"$*\" in *buildkit-registry-config*) cat " + fixture +
		" ;; *) echo 'NotFound' >&2; exit 1 ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runBackup(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{genSecrets, "--backup-only"}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeKubectl(t)+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestSecretBackupIsRestorableBecauseServerFieldsAreStripped(t *testing.T) {
	// Arrange
	home := t.TempDir()

	// Act
	out, err := runBackup(t, home, "build-secrets-dev", "argo/buildkit-registry-config", "argo/minio-credentials")

	// Assert
	if err != nil {
		t.Fatalf("backup failed: %v\n%s", err, out)
	}
	files, _ := filepath.Glob(filepath.Join(home, ".loopruntime", "backups", "build-secrets-dev-*.yaml"))
	if len(files) != 1 {
		t.Fatalf("want one backup file, got %v\n%s", files, out)
	}
	body, _ := os.ReadFile(files[0])
	s := string(body)
	if !strings.Contains(s, "name: buildkit-registry-config") || !strings.Contains(s, "token: ZmFrZQ==") {
		t.Errorf("backup lost the Secret itself:\n%s", s)
	}
	for _, server := range []string{"resourceVersion", "uid:", "creationTimestamp"} {
		if strings.Contains(s, server) {
			t.Errorf("backup keeps %s, so kubectl apply of it conflicts on restore:\n%s", server, s)
		}
	}
	if info, _ := os.Stat(files[0]); info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSecretBackupWritesNothingWhenNoSecretExists(t *testing.T) {
	// Arrange
	home := t.TempDir()

	// Act
	out, err := runBackup(t, home, "runtime-secrets-cell-0-dev", "loopruntime-system/cloudflared-tunnel-token")

	// Assert — a first install has nothing to back up, and that is not an error.
	if err != nil {
		t.Fatalf("backup of absent Secrets failed: %v\n%s", err, out)
	}
	if files, _ := filepath.Glob(filepath.Join(home, ".loopruntime", "backups", "*.yaml")); len(files) != 0 {
		t.Errorf("want no backup file, got %v", files)
	}
}

func TestSecretBackupRejectsAMissingSecretList(t *testing.T) {
	// Act
	out, err := runBackup(t, t.TempDir(), "only-a-prefix")

	// Assert
	if err == nil || !strings.Contains(out, "usage:") {
		t.Fatalf("want a usage error, got err=%v\n%s", err, out)
	}
}

func TestBuildAndRuntimeGeneratorsBackUpThroughTheSharedMode(t *testing.T) {
	for _, f := range []string{"generate-build-secrets.sh", "generate-runtime-secrets.sh"} {
		body, err := os.ReadFile(filepath.Join("../../helmfile/scripts", f))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "generate-secrets.sh\" --backup-only") {
			t.Errorf("%s does not back up through generate-secrets.sh --backup-only", f)
		}
	}
}
