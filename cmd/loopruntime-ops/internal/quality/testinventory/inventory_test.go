// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testinventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

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

// The tag is the author's declaration and beats the path — which is an inference by
// whoever wrote the classifier.
func TestBuildTagBeatsPath(t *testing.T) {
	// The path says unit; the tag says live.
	cat, reason := Classify("api/internal/x/foo_test.go", "//go:build live\n\npackage x\n")
	if cat != E2E {
		t.Fatalf("the tag declared by the author must beat the path; got %s (%s)", cat, reason)
	}
}

// `live` must not match inside `livetest` — substring matching would turn any tag that
// contains the other into a silent false positive.
func TestBuildTagMatchesWholeTokenOnly(t *testing.T) {
	if cat, _ := Classify("a/b_test.go", "//go:build livetest\n\npackage b\n"); cat == E2E {
		t.Fatal("`livetest` is not the `live` tag; matching has to be by whole token")
	}
}

func TestPathClassifiesWhenNoTag(t *testing.T) {
	cases := map[string]Category{
		"infra/tests/e2e/deploy_test.go":     E2E,
		"infra/tests/chaos/cell_test.go":     E2E,
		"infra/tests/charts/render_test.go":  Integration,
		"api/tests/repositories_test.go":     Integration,
		"api/internal/routes/health_test.go": Unit,
		"pkg/naming/naming_test.go":          Unit,
	}
	for path, want := range cases {
		if got, reason := Classify(path, "package x\n"); got != want {
			t.Errorf("%s → %s (%s); want %s", path, got, reason, want)
		}
	}
}

// The reason exists so a derived number can be checked. A derived inventory without
// provenance would have the same defect as the declared one.
func TestEveryClassificationCarriesItsReason(t *testing.T) {
	for _, in := range []string{"//go:build live\npackage x\n", "package x\n"} {
		if _, reason := Classify("infra/tests/e2e/a_test.go", in); reason == "" {
			t.Error("every classification must say which rule produced it")
		}
	}
}

func TestDeriveCountsAndRatios(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/a/a_test.go", "package a\n")
	write(t, root, "pkg/b/b_test.go", "package b\n")
	write(t, root, "infra/tests/e2e/e_test.go", "package e\n")
	write(t, root, "api/tests/i_test.go", "package i\n")
	write(t, root, "pkg/a/a.go", "package a\n") // not a test

	inv, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total() != 4 {
		t.Fatalf("total=%d; a non-test file does not enter the inventory", inv.Total())
	}
	if inv.Unit != 2 || inv.Integr != 1 || inv.E2E != 1 {
		t.Fatalf("unit=%d integr=%d e2e=%d", inv.Unit, inv.Integr, inv.E2E)
	}
	u, i, e := inv.Ratios()
	if u != 50 || i != 25 || e != 25 {
		t.Fatalf("wrong derived ratios: %.1f/%.1f/%.1f", u, i, e)
	}
}

// An empty tree returns zeros without dividing by zero — and the consumer decides what
// to do with that, instead of receiving NaN.
func TestEmptyTreeIsZeroNotNaN(t *testing.T) {
	inv, err := Derive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if u, i, e := inv.Ratios(); u != 0 || i != 0 || e != 0 {
		t.Fatalf("an empty tree must give 0/0/0; got %.1f/%.1f/%.1f", u, i, e)
	}
}

// `testdata/` and `node_modules/` hold fixture or third-party code. Counting them would
// make the pyramid describe someone else's tests.
func TestForeignAndFixtureTreesAreSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/a/a_test.go", "package a\n")
	write(t, root, "pkg/a/testdata/fixture_test.go", "package fixture\n")
	write(t, root, "web/node_modules/dep/d_test.go", "package d\n")

	inv, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total() != 1 {
		t.Fatalf("total=%d; fixtures and third-party dependencies are not this repo's suite: %+v", inv.Total(), inv.Entries)
	}
}

// The real population (B-109). The number that matters is that the derivation sees the
// WHOLE tree — the declared inventory saw 375 of 988, and that blindness was the defect.
func TestRealTreeIsFullyEnumerated(t *testing.T) {
	inv, err := Derive(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total() < 900 {
		t.Fatalf("total=%d — the tree has ~988 `_test.go` files; a number far below that means the scan is blind, which is exactly the defect B-106 removed", inv.Total())
	}
	if inv.E2E == 0 {
		t.Fatal("zero e2e in the real tree — `infra/tests/e2e/` exists, and the declared inventory said 1 of 710 precisely because it could not see it")
	}
}

// A test file whose header cannot be read would be classified on its path
// alone, silently dropping a build tag that decides its category.
func TestUnreadableTestFileIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "x_test.go")); err != nil {
		t.Fatal(err)
	}

	if _, err := Derive(root); err == nil {
		t.Fatal("an unreadable test file must be an error")
	}
}
