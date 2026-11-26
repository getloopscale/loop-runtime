// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package contractconsumers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// A consumer root that does not exist must surface, never vanish.
//
// `git grep -- a b c` silently ignores a pathspec that matches nothing. The two
// TypeScript roots — the wire consumers the Go compiler cannot see — left this
// repository at the decomposition and live in sibling checkouts that a clone
// may not have. When they are absent the verdict covers only Go, and it must
// say so (Partial) instead of reporting a full PASS.
func TestMissingConsumerRootIsReportedAndMakesTheVerdictPartial(t *testing.T) {
	root := filepath.Join(t.TempDir(), "loopruntime") // siblings would be ../loopruntime-*
	if err := os.MkdirAll(filepath.Join(root, "api", "internal"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.MissingRoots) != 2 || !out.Partial {
		t.Fatalf("want the two sibling roots missing and Partial=true, got missing=%v partial=%v",
			out.MissingRoots, out.Partial)
	}
	for _, r := range out.MissingRoots {
		if strings.Contains(r, "api/internal") {
			t.Errorf("api/internal was reported missing and it exists: %v", out.MissingRoots)
		}
	}
}

// A consumer root that exists but cannot be grepped is an error, not zero
// findings: the old code discarded `git grep`'s exit status entirely.
func TestGrepFailureInAConsumerRootIsAnError(t *testing.T) {
	root := t.TempDir() // not a git repository
	if err := os.MkdirAll(filepath.Join(root, "api", "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := grepConsumersForTag(root, "apps", []consumerRoot{{Repo: ".", Path: "api/internal/"}})
	if err == nil {
		t.Fatal("want an error when git grep cannot run, got nil")
	}
}

func TestGrepFindsReadsInASiblingCheckout(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "loopruntime")
	sibling := filepath.Join(parent, "loopruntime-cli")
	if err := os.MkdirAll(filepath.Join(sibling, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "src", "deploy.ts"), []byte("const n = p.apps;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = sibling
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	got, err := grepConsumersForTag(root, "apps", []consumerRoot{{Repo: "../loopruntime-cli", Path: "src/"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ConsumerFile != "../loopruntime-cli/src/deploy.ts" || got[0].ConsumerLine != 1 {
		t.Fatalf("want one finding at ../loopruntime-cli/src/deploy.ts:1, got %+v", got)
	}
}

// The real population: the declared roots measured against this workspace.
// A clone without the sibling checkouts misses at most the two sibling roots.
func TestRealConsumerRootPopulation(t *testing.T) {
	root := qtestutil.MustRepoRoot()
	missing := missingConsumerRoots(root)
	t.Logf("declared consumer roots: %d; missing here: %d %v", len(consumerRoots), len(missing), missing)
	if len(consumerRoots) == 0 {
		t.Fatal("no consumer root declared — green over nothing is not green")
	}
	for _, m := range missing {
		if !strings.HasPrefix(m, "../") {
			t.Errorf("an in-repository consumer root is missing: %s", m)
		}
	}
}
