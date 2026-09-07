package lintcoverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shared golangci-lint cache replayed the analysis of trees that no longer
// exist. Measured 2026-09-05 on the runner: `golangci-lint run ./...` inside
// the api module reported 35 findings, every one of them under two /tmp lane trees
// or /tmp/clone-B-059/api -- neither present on disk, neither a registered
// worktree. The same command with a private cache reported 0, and the gate turned
// the difference into `api [lint] 0 -> 35`, failing the whole chain on a module
// whose own lint is clean.

func TestEachModuleDirectoryGetsItsOwnCache(t *testing.T) {
	a := lintCacheDir("/repo/api")
	b := lintCacheDir("/tmp/some-lane/api")
	if a == b {
		t.Fatalf("two trees share a cache (%s); one tree's analysis can answer for the other", a)
	}
}

func TestTheCacheIsStableForTheSameDirectory(t *testing.T) {
	// Not isolated per run: the same tree must reuse its own cache, or the gate
	// pays a full cold lint every time and someone eventually turns it off.
	first := lintCacheDir("/repo/api")
	second := lintCacheDir("/repo/api")
	if first != second {
		t.Fatalf("cache is not stable: %s then %s", first, second)
	}
}

func TestTheCachePathIsAbsolute(t *testing.T) {
	got := lintCacheDir("/repo/api")
	if !filepath.IsAbs(got) {
		t.Fatalf("golangci-lint resolves a relative cache against its own cwd: %q", got)
	}
}

func TestTheCommandCarriesItsOwnCache(t *testing.T) {
	dir := "/repo/api"
	cmd := lintCommand(dir)
	if cmd.Dir != dir {
		t.Fatalf("command runs in %q, not %q", cmd.Dir, dir)
	}
	want := "GOLANGCI_LINT_CACHE=" + lintCacheDir(dir)
	var found bool
	for _, kv := range cmd.Env {
		if kv == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("the command does not carry %s; it would use the shared default", want)
	}
}

func TestTheCacheVariableIsSetExactlyOnceAndLast(t *testing.T) {
	// os.Environ() may already carry GOLANGCI_LINT_CACHE -- the runner exports one.
	// The appended value must be the one that wins, or the fix does nothing on the
	// machine where the problem lives.
	t.Setenv("GOLANGCI_LINT_CACHE", "/tmp/inherited-and-wrong")
	cmd := lintCommand("/repo/api")
	var last string
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "GOLANGCI_LINT_CACHE=") {
			last = kv
		}
	}
	if last != "GOLANGCI_LINT_CACHE="+lintCacheDir("/repo/api") {
		t.Fatalf("an inherited value wins over the derived one: last was %q", last)
	}
}

func TestTheCacheLivesUnderTheTempDirectory(t *testing.T) {
	got := lintCacheDir("/repo/api")
	if !strings.HasPrefix(got, os.TempDir()) {
		t.Fatalf("cache at %q escapes %q; a gate must not write into the tree it measures", got, os.TempDir())
	}
}
