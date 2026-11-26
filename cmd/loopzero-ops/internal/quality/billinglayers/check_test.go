// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// input is COMPOSED at run time.
//
// This file is swept by the very anchor it tests (EC-1). Spelling the input out here would
// turn the test into an undeclared file — and the lock would fail itself. This is the third
// time this trap has come up in the session; in the other two (`publishhygiene`,
// `trufflehogscan`) the way out was the same.
func input() string { return "loopzero-stripe" + "-webhook" }

// gitRepo creates a real repository: the gate reads `git ls-files`, which is what will be
// published, and not the working directory.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	return root
}

// base assembles a coherent repository: a registry, the layer file, the test that guards
// it, the taskfile and the backlog.
func base(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"infra/quality/billing-layers.txt": "[anchor]\n" + input() + "\n\n[floor]\n1\n\n" +
			"[layers]\nseed | loopzero | layer.go | test:guard_test.go | skips without a credential\n",
		"layer.go":              "const s = \"" + input() + "\"\n",
		"guard_test.go":         "package a\n",
		"taskfiles/quality.yml": "cmds:\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return gitRepo(t, files)
}

func run(t *testing.T, root, sibling string) (Outcome, error) {
	t.Helper()
	return violationOf(Check(Config{RepoRoot: root, SiblingRoot: sibling}))
}

func TestPassesWhenEveryGuardResolves(t *testing.T) {
	out, err := run(t, base(t, nil), "")
	if err != nil {
		t.Fatalf("a coherent registry passes: %v (%+v)", err, out)
	}
	if out.Layers != 1 || out.Scanned == 0 {
		t.Fatalf("counts: %+v", out)
	}
}

func TestBrokenGuardFailsNamingTheLayer(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[anchor]\n" + input() + "\n\n[floor]\n1\n\n" +
			"[layers]\nseed | loopzero | layer.go | test:deleted_test.go | skips without a credential\n",
	})
	out, err := run(t, root, "")
	if !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("a broken guard fails; err=%v", err)
	}
	if len(out.Broken) != 1 || out.Broken[0].Layer != "seed" {
		t.Fatalf("the layer must be NAMED to be fixable; got %+v", out.Broken)
	}
}

// The third DoD bullet: a new layer is born inside the check.
func TestUndeclaredFileReferencingAnInputFails(t *testing.T) {
	root := base(t, map[string]string{"new_layer.go": "const x = \"" + input() + "\"\n"})
	out, err := run(t, root, "")
	if !errors.Is(err, ErrUndeclaredSurface) {
		t.Fatalf("a new file with an input fails; err=%v", err)
	}
	if len(out.Undeclared) != 1 || out.Undeclared[0] != "new_layer.go" {
		t.Fatalf("the file must be named; got %+v", out.Undeclared)
	}
}

func TestDeclaredReferenceDoesNotFail(t *testing.T) {
	root := base(t, map[string]string{
		"new_layer.go": "const x = \"" + input() + "\"\n",
		"infra/quality/billing-layers.txt": "[anchor]\n" + input() + "\n\n[floor]\n1\n\n" +
			"[layers]\nseed | loopzero | layer.go | test:guard_test.go | skips without a credential\n\n" +
			"[references]\nnew_layer.go | it is the guard, not the layer\n",
	})
	if _, err := run(t, root, ""); err != nil {
		t.Fatalf("a reference declared with a reason does not fail: %v", err)
	}
}

// EC-1: the registry itself cites the inputs — it is their DEFINITION, not a use.
func TestRegistryDoesNotFailItself(t *testing.T) {
	if _, err := run(t, base(t, nil), ""); err != nil {
		t.Fatalf("the registry is not an undeclared layer: %v", err)
	}
}

// EC-4: deleting lines must not make the gate pass.
func TestBelowTheFloorFails(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[anchor]\n" + input() + "\n\n[floor]\n2\n\n" +
			"[layers]\nseed | loopzero | layer.go | test:guard_test.go | skips without a credential\n",
	})
	if _, err := run(t, root, ""); !errors.Is(err, ErrBelowFloor) {
		t.Fatalf("fewer layers than the floor fails; err=%v", err)
	}
}

// EC-8: present-but-incomplete is not absent.
func TestDeclaredFileThatDisappearedFails(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[anchor]\n" + input() + "\n\n[floor]\n1\n\n" +
			"[layers]\nseed | loopzero | gone.go | test:guard_test.go | skips without a credential\n",
	})
	if _, err := run(t, root, ""); !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("a declared file that does not exist fails; err=%v", err)
	}
}

// Partial coverage is REPORTED, never silenced — and never fails.
func TestAbsentSiblingReportsPartialNotGreen(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[anchor]\n" + input() + "\n\n[floor]\n2\n\n" +
			"[layers]\nseed | loopzero | layer.go | test:guard_test.go | skips without a credential\n" +
			"custo | loopzero-cloud | cmd/main.go | open:B-033 | switches off without prometheus\n",
	})
	out, err := run(t, root, "")
	if err != nil {
		t.Fatalf("an absent sibling does not fail: %v", err)
	}
	if !out.Partial || len(out.NotVerified) != 1 {
		t.Fatalf("what was not looked at must be NAMED; got %+v", out)
	}
	if !strings.Contains(out.Reason, "PARTIAL") {
		t.Fatalf("the reason must say coverage is partial: %q", out.Reason)
	}
}

func TestMissingRegistryFails(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	if _, err := run(t, root, ""); !errors.Is(err, ErrRegistryMalformed) {
		t.Fatalf("a missing registry fails; err=%v", err)
	}
}

// The real population (rules/testing.md § 4.2).
func TestRealTreePasses(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: repoRoot, SiblingRoot: filepath.Join(repoRoot, "..", "loopzero-cloud")}))
	if err != nil {
		for i, b := range out.Broken {
			if i == 8 {
				break
			}
			t.Errorf("guard: %s — %s", b.Layer, b.Why)
		}
		for i, u := range out.Undeclared {
			if i == 8 {
				break
			}
			t.Errorf("undeclared: %s", u)
		}
		t.Fatalf("real tree: %v", err)
	}
	if out.Scanned == 0 || out.Layers == 0 {
		t.Fatalf("a sweep that reached nothing must not report green: %+v", out)
	}
}

// The debt ledger is versioned: the gate resolves `open:` guards from the
// registry's [debts] section and never from BACKLOG.md, which is gitignored
// and absent on every clone.
func TestOpenDebtResolvesFromTheVersionedRegistryWithoutABacklog(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[anchor]\n" + input() + "\n\n[floor]\n1\n\n" +
			"[layers]\nseed | loopzero | layer.go | open:B-033 | skips without a credential\n\n" +
			"[debts]\nB-033 | approved | the cost ceiling ships with a gate\n",
	})
	if _, err := os.Stat(filepath.Join(root, "BACKLOG.md")); err == nil {
		t.Fatal("fixture must not carry a BACKLOG.md")
	}
	if _, err := run(t, root, ""); err != nil {
		t.Fatalf("an open debt declared in the registry resolves: %v", err)
	}
}

// A versioned file the sweep cannot read may be exactly the new layer it
// exists to catch; it is an error, never a file without billing inputs.
func TestUnreadableVersionedFileFailsTheSweep(t *testing.T) {
	root := base(t, nil)
	// A tracked dangling symlink is unreadable for every uid, root included.
	if err := os.Symlink("does-not-exist.go", filepath.Join(root, "locked.go")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "locked.go")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if _, err := run(t, root, ""); err == nil || !strings.Contains(err.Error(), "locked.go") {
		t.Fatalf("want an error naming the unreadable file, got %v", err)
	}
}
