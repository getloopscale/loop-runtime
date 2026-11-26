// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package noaesremaining

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheck_FailsClosedOnUnreadableFile: a Go file the walk counts as found but
// cannot read must fail the gate. Recording it as a warning and computing Pass
// from the hits alone let an unread file vouch for itself — the AES path could
// sit in exactly that file. A dangling symlink is unreadable for every user, root
// included.
func TestCheck_FailsClosedOnUnreadableFile(t *testing.T) {
	tmp := t.TempDir()
	seedNonAPIRoots(t, tmp)
	writeGo(t, tmp, canonicalStoreFile, "package store\nfunc encryptForStore() string { return \"vault:v1:ct\" }\n")
	link := filepath.Join(tmp, "api", "internal", "db", "store", "legacy.go")
	if err := os.Symlink(filepath.Join(tmp, "missing-target"), link); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})

	if err == nil {
		t.Fatalf("want fail-closed error on an unreadable Go file, got nil and Pass=%v", out.Pass)
	}
	if !strings.Contains(err.Error(), "legacy.go") {
		t.Errorf("error %q does not name the file it could not read", err)
	}
}
