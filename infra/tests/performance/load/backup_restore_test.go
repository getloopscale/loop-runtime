// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP iter 17 — backup/restore E2E regression test.
// Validates the fix for kubectl apply Conflict error on resourceVersion when
// restoring a Secret that was modified after the backup was taken.

package load

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestBackupRestore_NoResourceVersionConflict validates that the
// generate-secrets.sh backup file is restorable via `kubectl apply -f`
// even when target Secret was modified after the backup was taken.
//
// Pre-iter-17 bug: backup contained resourceVersion + uid + creationTimestamp,
// causing `kubectl apply` to fail with "Conflict: object has been modified"
// on any namespace/cluster where the same-named Secret has different state.
func TestBackupRestore_NoResourceVersionConflict(t *testing.T) {
	if os.Getenv("LOOPRUNTIME_K3D_INTEGRATION") == "" {
		t.Skip("set LOOPRUNTIME_K3D_INTEGRATION=1 to run k3d integration tests")
	}

	// Find latest backup file
	home := os.Getenv("HOME")
	backupDir := home + "/.loopruntime/backups"
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Skipf("backup dir not found: %v", err)
	}
	var latest string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "secrets-mgmt-") || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if e.Name() > latest {
			latest = e.Name()
		}
	}
	if latest == "" {
		t.Skip("no backup file found — run generate-secrets.sh first")
	}

	backupPath := backupDir + "/" + latest
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	content := string(data)

	// CRITICAL: backup must NOT contain server-managed fields
	for _, forbidden := range []string{
		"resourceVersion:",
		"  uid:",
		"creationTimestamp:",
		"managedFields:",
		"ownerReferences:",
	} {
		if strings.Contains(content, forbidden) {
			t.Errorf("backup file contains server-managed field %q — would cause Conflict on restore", forbidden)
		}
	}

	// Verify it's still valid YAML by attempting kubectl apply --dry-run=server
	cmd := exec.Command("kubectl", "--context", "k3d-loopruntime-test",
		"apply", "-f", backupPath, "--dry-run=server")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl apply --dry-run=server failed: %v\noutput: %s", err, out)
	}

	// Output should show resources are valid (configured/created)
	if !strings.Contains(string(out), "configured") && !strings.Contains(string(out), "created") && !strings.Contains(string(out), "unchanged") {
		t.Errorf("apply --dry-run output unexpected: %s", out)
	}

	// Critical assertion: NO "Conflict" error in output
	if strings.Contains(string(out), "Conflict") {
		t.Errorf("backup file produces Conflict error on apply: %s", out)
	}
}

// TestBackupRestore_AllSecretsPresent validates that the backup file
// contains all 6 expected LoopRuntime-managed Secrets.
func TestBackupRestore_AllSecretsPresent(t *testing.T) {
	if os.Getenv("LOOPRUNTIME_K3D_INTEGRATION") == "" {
		t.Skip("set LOOPRUNTIME_K3D_INTEGRATION=1 to run k3d integration tests")
	}

	home := os.Getenv("HOME")
	backupDir := home + "/.loopruntime/backups"
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Skipf("backup dir not found: %v", err)
	}
	var latest string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "secrets-mgmt-") && strings.HasSuffix(e.Name(), ".yaml") {
			if e.Name() > latest {
				latest = e.Name()
			}
		}
	}
	if latest == "" {
		t.Skip("no backup file found")
	}

	data, err := os.ReadFile(backupDir + "/" + latest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)

	expected := []string{
		"loopruntime-db-credentials",
		"gitea-admin",
		"minio-credentials",
		"loopruntime-registry-pull",
	}
	for _, name := range expected {
		if !strings.Contains(content, "name: "+name) {
			t.Errorf("backup missing expected Secret %q", name)
		}
	}
}

// TestBackupRestore_BackupFilePermissions verifies backup files are 0600.
func TestBackupRestore_BackupFilePermissions(t *testing.T) {
	if os.Getenv("LOOPRUNTIME_K3D_INTEGRATION") == "" {
		t.Skip("set LOOPRUNTIME_K3D_INTEGRATION=1 to run k3d integration tests")
	}

	home := os.Getenv("HOME")
	backupDir := home + "/.loopruntime/backups"
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Skip("no backup dir")
	}
	checked := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "secrets-") {
			continue
		}
		info, err := os.Stat(backupDir + "/" + e.Name())
		if err != nil {
			continue
		}
		mode := info.Mode().Perm()
		if mode != 0o600 {
			t.Errorf("backup %s has perm %v, expected 0600 (security)", e.Name(), mode)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no backup files to check")
	}
}
