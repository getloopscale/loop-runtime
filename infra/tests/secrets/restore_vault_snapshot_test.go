// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restore-vault-snapshot.sh (Runbook P5 of plan 2026-06-02-loopruntime-api-vault-integration)
// restores OpenBao from a raft snapshot in object storage. Restoring overwrites the
// live state, so the invariant that matters most is the default: without --execute
// the script must neither copy the snapshot into the pod nor run the restore.

const restoreScript = openbaoScripts + "/restore-vault-snapshot.sh"

// restoreSnapshot is the key every restore case asks for; the fake listing holds it.
const restoreSnapshot = "2026-06-01T01-00-00Z.snap"

// restoreAWS is the environment every non-pre-flight case needs. The values are
// placeholders: the fake aws never reads them.
var restoreAWS = []string{"AWS_ACCESS_KEY_ID=fixture", "AWS_SECRET_ACCESS_KEY=fixture"}

func init() {
	registerFakes("restore-vault-snapshot", map[string]fakeTool{
		"aws":     restoreAWSCLI,
		"kubectl": restoreKubectl,
	})
}

func restoreAWSCLI(args []string) int {
	recordCall("FAKE_AWS_LOG", "AWS_CALL", args)
	switch {
	case argsMatch(args, "s3 ls"):
		if code := envExit("FAKE_AWS_LS_EXIT"); code != 0 {
			return code
		}
		for _, day := range []string{"2026-06-03", "2026-06-02", "2026-06-01", "2026-05-31", "2026-05-30"} {
			fmt.Printf("%s 01:00:00      1024 %sT01-00-00Z.snap\n", day, day)
		}
		return 0
	case argsMatch(args, "s3 cp"):
		target := args[len(args)-1]
		if err := os.WriteFile(target, []byte("fixture snapshot bytes\n"), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "fake aws: write %s: %v\n", target, err)
			return 1
		}
		return envExit("FAKE_AWS_CP_EXIT")
	default:
		return 0
	}
}

func restoreKubectl(args []string) int {
	recordCall("FAKE_KUBECTL_LOG", "KUBECTL_CALL", args)
	switch {
	case argsMatch(args, "version --request-timeout"):
		return 0
	case argsMatch(args, "cp "):
		return envExit("FAKE_KUBECTL_CP_EXIT")
	case argsMatch(args, "exec ", "bao operator raft snapshot restore"):
		return envExit("FAKE_BAO_RESTORE_EXIT")
	default:
		return 0
	}
}

func runRestore(t *testing.T, env []string, args ...string) scriptRun {
	t.Helper()
	return runScript(t, restoreScript, "restore-vault-snapshot", env, args...)
}

func TestRestoreVaultSnapshotRequiresAnEnv(t *testing.T) {
	r := runRestore(t, restoreAWS, "--list")
	expectExit(t, r, 1, "env required")
}

func TestRestoreVaultSnapshotRequiresTheAWSAccessKeyID(t *testing.T) {
	r := runRestore(t, []string{"AWS_SECRET_ACCESS_KEY=fixture"}, "--env", "vault-local", "--list")
	expectExit(t, r, 1, "AWS_ACCESS_KEY_ID")
}

func TestRestoreVaultSnapshotRequiresAListOrASnapshotKey(t *testing.T) {
	r := runRestore(t, restoreAWS, "--env", "vault-local")
	expectExit(t, r, 1, "must provide --list OR --snapshot-key")
}

func TestRestoreVaultSnapshotRefusesListAndSnapshotKeyTogether(t *testing.T) {
	r := runRestore(t, restoreAWS, "--env", "vault-local", "--list", "--snapshot-key", "foo.snap")
	expectExit(t, r, 1, "mutually exclusive")
}

// The safety invariant: the default mode is a dry run, and it touches neither the
// pod's filesystem nor the raft state.
func TestRestoreVaultSnapshotDefaultModeRunsNothingDestructive(t *testing.T) {
	log := filepath.Join(t.TempDir(), "kubectl.log")
	r := runRestore(t, append([]string{"FAKE_KUBECTL_LOG=" + log}, restoreAWS...),
		"--env", "vault-local", "--snapshot-key", restoreSnapshot)

	expectExit(t, r, 0, "DRY-RUN MODE")
	if n := countCalls(t, log, "bao operator raft snapshot restore"); n != 0 {
		t.Errorf("the default mode ran the restore %d time(s); calls: %v", n, calls(t, log))
	}
	if n := countCalls(t, log, "cp "); n != 0 {
		t.Errorf("the default mode copied into the pod %d time(s); calls: %v", n, calls(t, log))
	}
}

func TestRestoreVaultSnapshotListPrintsTheSnapshots(t *testing.T) {
	r := runRestore(t, restoreAWS, "--env", "vault-local", "--list")
	expectExit(t, r, 0)
	for _, want := range []string{"2026-06-03T01-00-00Z.snap", "2026-05-30T01-00-00Z.snap"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("--list stdout does not name %s\n%s", want, r)
		}
	}
}

func TestRestoreVaultSnapshotExecuteCopiesAndRestores(t *testing.T) {
	log := filepath.Join(t.TempDir(), "kubectl.log")
	r := runRestore(t, append([]string{"FAKE_KUBECTL_LOG=" + log}, restoreAWS...),
		"--env", "vault-local", "--snapshot-key", restoreSnapshot, "--execute")

	expectExit(t, r, 0)
	if countCalls(t, log, "bao operator raft snapshot restore") < 1 {
		t.Errorf("--execute did not run the restore; calls: %v", calls(t, log))
	}
	if countCalls(t, log, "cp ") < 1 {
		t.Errorf("--execute did not copy the snapshot into the pod; calls: %v", calls(t, log))
	}
}

func TestRestoreVaultSnapshotExitsTwoWhenTheSnapshotIsNotInTheBucket(t *testing.T) {
	r := runRestore(t, append([]string{"FAKE_AWS_LS_EXIT=1"}, restoreAWS...),
		"--env", "vault-local", "--snapshot-key", "nonexistent.snap")
	expectExit(t, r, 2, "snapshot not found")
}

// A restored OpenBao comes back sealed; the completion message must say so, or the
// operator reads COMPLETE and walks away from a platform that cannot read a secret.
func TestRestoreVaultSnapshotCompletionNamesTheUnsealCeremony(t *testing.T) {
	r := runRestore(t, restoreAWS, "--env", "vault-local", "--snapshot-key", restoreSnapshot, "--execute")
	expectExit(t, r, 0, "Vault state restored", "Shamir unseal ceremony")
}
