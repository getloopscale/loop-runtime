package installgithooks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeExec(t *testing.T, p, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(body), 0o755)
}

func TestCheck_HappyPath_PASS(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/scripts/local/git-hooks/pre-push"),
		"#!/bin/bash\nif [ \"${LOOPZERO_SKIP_PREPUSH:-}\" = 1 ]; then exit 0; fi\ntask quality:gates\n")
	writeExec(t, filepath.Join(root, "infra/scripts/local/install-git-hooks.sh"),
		"#!/bin/bash\ninstall -m 0755 hook dst\n")
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("expected PASS, got %v", err)
	}
}

func TestCheck_MissingHook_FAIL(t *testing.T) {
	root := t.TempDir()
	// only installer, no hook
	writeExec(t, filepath.Join(root, "infra/scripts/local/install-git-hooks.sh"),
		"#!/bin/bash\ninstall -m 0755 x y\n")
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrFailures) {
		t.Fatalf("expected ErrFailures, got %v", err)
	}
}

func TestCheck_HookMissingEscape_FAIL(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/scripts/local/git-hooks/pre-push"),
		"#!/bin/bash\ntask quality:gates\n") // missing LOOPZERO_SKIP_PREPUSH
	writeExec(t, filepath.Join(root, "infra/scripts/local/install-git-hooks.sh"),
		"#!/bin/bash\ninstall -m 0755 x y\n")
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrFailures) {
		t.Fatalf("expected ErrFailures, got %v", err)
	}
}
