// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package clusterrenameguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// `git grep` exiting 1 is an ANSWER, not a failure to ask.
//
// existsAnywhere ran `git grep --fixed-strings --quiet` and fell back to a full-tree walk
// whenever it returned a non-zero status. But exit 1 is git saying "not found" — an
// authoritative answer — and only a higher status, or the binary being absent, means the
// question could not be put.
//
// Collapsing the two made every NOT-FOUND occurrence trigger a walk, and the walk read
// every file under the root with no directory skipping. Measured 2026-09-16: `.git` here
// is 554 MB, the baseline holds ~1,388 occurrences, and the gate took over six minutes in
// the pre-push path — where `cq_invoke` already recorded what a slow gate becomes ("a
// 22-minute gate is a gate people bypass").
//
// This asserts the distinction directly: in a real repository, a token that is absent is
// answered by git, and the fallback never runs.
func TestAbsentTokenIsAnsweredByGitNotByWalking(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH — this test is about preferring git's answer over a walk; https://github.com/getloopscale/loop-runtime/issues/369")
	}
	root := t.TempDir()
	mustRun(t, root, "git", "init", "-q")
	write(t, filepath.Join(root, "a.txt"), "nothing interesting here\n")
	mustRun(t, root, "git", "add", "-A")

	// A decoy the WALK would find and git grep would not: it lives inside .git, which is
	// the object store rather than the working tree. If the walk runs, this is reported
	// as present, which is the wrong answer as well as the slow one.
	write(t, filepath.Join(root, ".git", "decoy-blob"), "mgmt-cluster-token\n")

	if existsAnywhere(root, "baseline.json", "mgmt-cluster-token") {
		t.Fatal("a token present only inside .git must not count as present in the tree")
	}
}

// And a token that IS in the tree is still found.
func TestPresentTokenIsFound(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH; https://github.com/getloopscale/loop-runtime/issues/369")
	}
	root := t.TempDir()
	mustRun(t, root, "git", "init", "-q")
	write(t, filepath.Join(root, "a.txt"), "core-cluster lives here\n")
	mustRun(t, root, "git", "add", "-A")

	if !existsAnywhere(root, "baseline.json", "core-cluster lives here") {
		t.Fatal("a token in a tracked file must be found")
	}
}

// Without git the walk is the only instrument left, and it must still skip the trees that
// are not the working tree.
func TestWalkFallbackSkipsTheObjectStore(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".git", "objects", "ab", "cdef"), "mgmt-cluster-token\n")
	write(t, filepath.Join(root, "node_modules", "x", "index.js"), "mgmt-cluster-token\n")
	if walkFinds(root, "baseline.json", "mgmt-cluster-token") {
		t.Fatal("the walk must skip .git and node_modules — neither is the working tree")
	}
	write(t, filepath.Join(root, "real.yaml"), "mgmt-cluster-token\n")
	if !walkFinds(root, "baseline.json", "mgmt-cluster-token") {
		t.Fatal("the walk must still find a token in a real file")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
