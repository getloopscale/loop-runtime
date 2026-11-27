// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"fmt"
	"os"
	"testing"
)

// enable-vault-backup.sh (Runbook P4 of plan 2026-06-02-loop-runtime-api-vault-integration)
// writes the snapshot agent's S3 credentials into a Secret, syncs the agent with
// helmfile and verifies the CronJob. It handles two credentials, so besides the
// pre-flight refusals it must never print either of them.

const enableBackupScript = openbaoScripts + "/enable-vault-backup.sh"

// The fixture credentials are recognisable strings, not credential-shaped ones:
// what the leak test looks for is that the script never echoes them.
const (
	backupAccessKeyFixture = "fixture-backup-access-key-id"
	backupSecretKeyFixture = "fixture-backup-secret-access-key"
)

func init() {
	registerFakes("enable-vault-backup", map[string]fakeTool{
		"kubectl":  backupKubectl,
		"helmfile": func([]string) int { return envExit("FAKE_HELMFILE_EXIT") },
	})
}

func backupKubectl(args []string) int {
	switch {
	case argsMatch(args, "version --request-timeout"):
		return 0
	case argsMatch(args, "create secret", "--dry-run=client"):
		fmt.Println("apiVersion: v1\nkind: Secret\nmetadata:\n  name: openbao-snapshot-s3-creds")
		return 0
	case argsMatch(args, "apply -f -"):
		drainStdin()
		return envExit("FAKE_APPLY_EXIT")
	case argsMatch(args, "get cronjob"):
		schedule := os.Getenv("FAKE_CRONJOB_SCHEDULE")
		if schedule == "" {
			schedule = "0 * * * *"
		}
		fmt.Println(schedule)
		return 0
	default:
		return 0
	}
}

// backupFixtures writes the three credentials files the cases need and an empty
// helmfile, and returns their directory.
func backupFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	access := "OPENBAO_BACKUP_ACCESS_KEY_ID=\"" + backupAccessKeyFixture + "\"\n"
	secret := "OPENBAO_BACKUP_SECRET_ACCESS_KEY=\"" + backupSecretKeyFixture + "\"\n"
	writeFixture(t, dir, "creds.env", access+secret)
	writeFixture(t, dir, "creds_no_access.env", secret)
	writeFixture(t, dir, "creds_no_secret.env", access)
	writeFixture(t, dir, "secrets.helmfile.yaml.gotmpl", "")
	return dir
}

// runEnableBackup runs the script with a context and a helmfile path, and the
// given credentials file from the fixtures.
func runEnableBackup(t *testing.T, creds string, env []string, args ...string) scriptRun {
	t.Helper()
	dir := backupFixtures(t)
	return runScript(t, enableBackupScript, "enable-vault-backup", env, append([]string{
		"--env", "vault-local",
		"--creds-file", dir + "/" + creds,
		"--vault-context", "k3d-mock",
		"--helmfile-path", dir + "/secrets.helmfile.yaml.gotmpl",
	}, args...)...)
}

func TestEnableVaultBackupRequiresAnEnv(t *testing.T) {
	r := runScript(t, enableBackupScript, "enable-vault-backup", nil)
	expectExit(t, r, 1, "env required")
}

func TestEnableVaultBackupRefusesAMissingCredsFile(t *testing.T) {
	r := runEnableBackup(t, "nonexistent.env", nil)
	expectExit(t, r, 1, "creds file not found")
}

func TestEnableVaultBackupRefusesCredsWithoutTheAccessKeyID(t *testing.T) {
	r := runEnableBackup(t, "creds_no_access.env", []string{"SKIP_PREFLIGHT=0"})
	expectExit(t, r, 1, "OPENBAO_BACKUP_ACCESS_KEY_ID not set")
}

func TestEnableVaultBackupRefusesCredsWithoutTheSecretAccessKey(t *testing.T) {
	r := runEnableBackup(t, "creds_no_secret.env", []string{"SKIP_PREFLIGHT=0"})
	expectExit(t, r, 1, "OPENBAO_BACKUP_SECRET_ACCESS_KEY not set")
}

func TestEnableVaultBackupDryRunNamesAllThreeSteps(t *testing.T) {
	r := runEnableBackup(t, "creds.env", []string{"SKIP_PREFLIGHT=1"}, "--dry-run")
	expectExit(t, r, 0, "STEP 1/3", "STEP 2/3", "STEP 3/3", "DRY-RUN COMPLETE")
}

func TestEnableVaultBackupDryRunNeverPrintsTheCredentials(t *testing.T) {
	r := runEnableBackup(t, "creds.env", []string{"SKIP_PREFLIGHT=1"}, "--dry-run")
	expectExit(t, r, 0)
	refuseLeak(t, r, backupAccessKeyFixture, backupSecretKeyFixture)
}

func TestEnableVaultBackupSkipHelmfileSkipsTheSync(t *testing.T) {
	r := runEnableBackup(t, "creds.env", []string{"SKIP_PREFLIGHT=0"}, "--skip-helmfile")
	expectExit(t, r, 0, "SKIPPED")
}

func TestEnableVaultBackupCompletesOnTheHappyPath(t *testing.T) {
	r := runEnableBackup(t, "creds.env", []string{"SKIP_PREFLIGHT=0"})
	expectExit(t, r, 0, "Vault backup enabled")
}

func TestEnableVaultBackupExitsTwoWhenTheSecretApplyFails(t *testing.T) {
	r := runEnableBackup(t, "creds.env", []string{"SKIP_PREFLIGHT=0", "FAKE_APPLY_EXIT=1"})
	expectExit(t, r, 2, "Secret apply failed")
}

// A failed sync names the flag the operator most likely forgot, rather than
// leaving them to read helmfile's output for it.
func TestEnableVaultBackupNamesTheAgentFlagWhenTheSyncFails(t *testing.T) {
	r := runEnableBackup(t, "creds.env", []string{"SKIP_PREFLIGHT=0", "FAKE_HELMFILE_EXIT=1"})
	expectExit(t, r, 2, "snapshotAgentEnabled")
}
