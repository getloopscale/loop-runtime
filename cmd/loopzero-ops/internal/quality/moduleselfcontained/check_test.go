// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package moduleselfcontained

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

type fakeBuilder struct {
	failures map[string]error
	seen     []string
}

func (f *fakeBuilder) BuildWithoutWorkspace(_, mod string) error {
	f.seen = append(f.seen, mod)
	return f.failures[mod]
}

func tree(t *testing.T, mods ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, m := range mods {
		p := filepath.Join(root, m, "go.mod")
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// THE DEFECT MEASURED on 2026-08-18: `operators/api/go.mod` bumped k8s to v0.36.0 in a CVE
// fix on Aug 13, and `operators/go.mod` — which depends on it — stayed at v0.35.3. `go.work`
// resolves across modules and masks it; the Dockerfile uses `GOWORK=off` and does not. CI
// stopped publishing ANY image for five days, and nothing failed.
func TestModuleThatOnlyBuildsWithWorkspaceFails(t *testing.T) {
	root := tree(t, "operators", "api")
	b := &fakeBuilder{failures: map[string]error{
		"operators": errors.New("go: updates to go.mod needed; to update it:\n\tgo mod tidy"),
	}}
	out, err := violationOf(Check(Config{RepoRoot: root, Builder: b}))
	if !errors.Is(err, ErrNotSelfContained) {
		t.Fatalf("a module that only builds with go.work fails; err=%v", err)
	}
	if len(out.Failed) != 1 || out.Failed[0].Module != "operators" {
		t.Fatalf("the finding must NAME the module: %+v", out.Failed)
	}
}

// All are built even when one fails — the same bullet as B-121: aborting at the first
// hides the ones after it.
func TestFailureInOneModuleDoesNotStopTheOthers(t *testing.T) {
	root := tree(t, "api", "operators", "pkg")
	b := &fakeBuilder{failures: map[string]error{"api": errors.New("boom")}}
	_, _ = violationOf(Check(Config{RepoRoot: root, Builder: b}))
	if len(b.seen) != 3 {
		t.Fatalf("all three must be built; seen=%v", b.seen)
	}
}

func TestAllSelfContainedPasses(t *testing.T) {
	root := tree(t, "api", "pkg")
	out, err := violationOf(Check(Config{RepoRoot: root, Builder: &fakeBuilder{}}))
	if err != nil {
		t.Fatalf("all self-contained passes: %v", err)
	}
	if out.Checked != 2 {
		t.Fatalf("checked=%d", out.Checked)
	}
}

func TestZeroModulesFails(t *testing.T) {
	if _, err := violationOf(Check(Config{RepoRoot: t.TempDir(), Builder: &fakeBuilder{}})); err == nil {
		t.Fatal("zero modules scanned must fail")
	}
}

// The real population — slow on purpose: it is what proves the invariant on the tree that ships.
func TestRealTree(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: repoRoot}))
	if err != nil {
		for _, f := range out.Failed {
			t.Errorf("%s does not build without go.work:\n%s", f.Module, f.Err)
		}
		t.Fatalf("real tree: %v", err)
	}
	if out.Checked == 0 {
		t.Fatal("zero modules checked — finding nothing is not the same as finding nothing wrong")
	}
}
