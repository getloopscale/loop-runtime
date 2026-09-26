// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package installgithooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExec(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCheck_HappyPath_PASS(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/scripts/local/git-hooks/pre-push"),
		"#!/bin/bash\nif [ \"${LOOPZERO_SKIP_PREPUSH:-}\" = 1 ]; then exit 0; fi\ntask quality:gates\n")
	writeExec(t, filepath.Join(root, "infra/scripts/local/install-git-hooks.sh"),
		"#!/bin/bash\ninstall -m 0755 hook dst\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("expected PASS, got %v", err)
	}
}

func TestCheck_MissingHook_FAIL(t *testing.T) {
	root := t.TempDir()
	// only installer, no hook
	writeExec(t, filepath.Join(root, "infra/scripts/local/install-git-hooks.sh"),
		"#!/bin/bash\ninstall -m 0755 x y\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrFailures) {
		t.Fatalf("expected ErrFailures, got %v", err)
	}
}

func TestCheck_HookMissingEscape_FAIL(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/scripts/local/git-hooks/pre-push"),
		"#!/bin/bash\ntask quality:gates\n") // missing LOOPZERO_SKIP_PREPUSH
	writeExec(t, filepath.Join(root, "infra/scripts/local/install-git-hooks.sh"),
		"#!/bin/bash\ninstall -m 0755 x y\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrFailures) {
		t.Fatalf("expected ErrFailures, got %v", err)
	}
}

// A hook that cannot be read is reported as unreadable, not as a hook that
// happens to lack every expected line.
func TestCheck_UnreadableHook_NamesTheReadFailure(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/scripts/local/install-git-hooks.sh"),
		"#!/bin/bash\ninstall -m 0755 x y\n")
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrFailures) {
		t.Fatalf("expected ErrFailures, got %v", err)
	}
	if !strings.Contains(strings.Join(out.Failures, "\n"), "read hook") {
		t.Fatalf("want a failure naming the read error, got %v", out.Failures)
	}
}
