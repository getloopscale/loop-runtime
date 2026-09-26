// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package publishhygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitTreeWithSymlink commits a tree holding a symlink, which git stores as the
// link's target text (mode 120000), not as whatever the target contains.
func gitTreeWithSymlink(t *testing.T, target string) string {
	t.Helper()
	root := gitTree(t, map[string]string{"a.md": "clean\n", "shared/dir/x.yaml": "clean\n"})
	if err := os.MkdirAll(filepath.Join(root, "chart"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "chart", "rules")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v — %s", err, out)
	}
	return root
}

func TestVersionedSymlinkToADirectoryIsScannedAsItsTarget(t *testing.T) {
	root := gitTreeWithSymlink(t, "../shared/dir")

	out, err := violationOf(Check(Config{RepoRoot: root}))

	if err != nil || len(out.Unverified) != 0 {
		t.Fatalf("a versioned symlink publishes its target text, which is readable; out=%+v err=%v", out, err)
	}
}

func TestVersionedSymlinkTargetCarryingAHomePathIsALeak(t *testing.T) {
	root := gitTreeWithSymlink(t, realHome("dashboards"))

	out, err := violationOf(Check(Config{RepoRoot: root}))

	if len(out.Findings) != 1 || out.Findings[0].File != "chart/rules" {
		t.Fatalf("the symlink target is what git publishes and it names a workstation path; out=%+v err=%v", out, err)
	}
}
