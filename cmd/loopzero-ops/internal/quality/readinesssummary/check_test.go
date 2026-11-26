// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package readinesssummary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The mutant. A `.status` persist with no fold at all — neither inline nor via a
// `defer` in the caller. Without this proof, a green from the gate would be
// indistinguishable from a gate that cannot see the shape.
func TestFailsPersistWithNoFoldAtAll(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/x/c.go", `package x

func (r *R) reconcileSomething() error {
	return r.Status().Patch(nil, nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: root, Packages: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a persist with no fold passed — the gate cannot see the shape it exists to watch")
	}
	if len(out.Violations) != 1 {
		t.Fatalf("wanted 1 violation, got %d: %+v", len(out.Violations), out.Violations)
	}
}

// The NEW capability, and the reason this gate exists: the fold via `defer` happens in
// RECONCILE, and the persist lives in a helper it calls. The old guard
// (`exposure_status_writer_test.go`) looks only at the function that encloses the `Patch`,
// so it counted this shape as absence — that is how `preview` measured "0 of 6" while
// having coverage (B-138). `DeferReadinessSummary` re-fetches the CR and PERSISTS the
// summary on return, that is, it is the last write; the helper's persist is covered.
func TestAcceptsFoldViaDeferInTheCaller(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/x/c.go", `package x

func (r *R) Reconcile() error {
	defer reconcileloop.DeferReadinessSummary(nil, nil, nil)()
	return r.fail()
}

func (r *R) fail() error {
	return r.Status().Patch(nil, nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: root, Packages: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("a fold via defer in the caller was failed; it is the canonical shape of the five reconcilers: %+v", out.Violations)
	}
}

// The inline fold covers ONLY its own function — it is a mutation before that
// function's write, and says nothing about what another function writes.
func TestInlineFoldDoesNotCoverAnotherFunction(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/x/c.go", `package x

func (r *R) writesWithFold() error {
	SetReadinessSummaryViaPkg(nil, nil)
	return r.Status().Patch(nil, nil, nil)
}

func (r *R) writesWithoutFold() error {
	return r.Status().Update(nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: root, Packages: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("one function's inline fold covered the persist of ANOTHER — that is not the invariant")
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0].Func, "writesWithoutFold") {
		t.Errorf("the violation should be writesWithoutFold: %+v", out.Violations)
	}
}

// The real population: the six operators packages. A mutant proves the detector
// fires; only this says what it finds in what exists (`rules/testing.md` § 4.2).
// The result is recorded right here, even when it is zero.
func TestRealPopulationOfTheSixPackages(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Packages < 6 {
		t.Fatalf("scanned %d package(s); B-138's declared population is 6", out.Packages)
	}
	if out.Sites == 0 {
		t.Fatal("zero persists found — green over nothing is not green")
	}
	t.Logf("population: %d packages, %d persists, %d covered, %d violation(s)",
		out.Packages, out.Sites, out.Sites-len(out.Violations), len(out.Violations))
	for _, v := range out.Violations {
		t.Logf("  uncovered: %s:%d %s (%s)", v.File, v.Line, v.Func, v.Shape)
	}
}

// Two types in the SAME package with a same-named method. It is the real shape of
// `operators/internal/app`, which has two `Reconcile` — Application and Exposure — and
// only Exposure carries the `defer`. If the graph indexes by NAME, one's defer covers
// the other's helpers and the gate reports green over an uncovered persist.
func TestDoesNotConfuseSameNamedMethodsOfDifferentTypes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/x/c.go", `package x

func (r *WithDefer) Reconcile() error {
	defer reconcileloop.DeferReadinessSummary(nil, nil, nil)()
	return r.persist()
}

func (r *WithDefer) persist() error {
	return r.Status().Patch(nil, nil, nil)
}

func (r *WithoutDefer) Reconcile() error {
	return r.save()
}

func (r *WithoutDefer) save() error {
	return r.Status().Update(nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: root, Packages: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("one type's defer covered ANOTHER type's helper — the graph is indexed by name and leaks")
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0].Func, "save") {
		t.Errorf("the violation should be WithoutDefer.save: %+v", out.Violations)
	}
}
