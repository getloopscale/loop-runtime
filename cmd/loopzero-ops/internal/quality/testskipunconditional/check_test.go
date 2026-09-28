// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testskipunconditional

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- fixture plumbing -------------------------------------------------------

const pinHeader = "# fixture pin\n#\n"

// tree writes a fixture repository and returns its root. files maps
// repo-relative paths to contents; pin is the pin file's body (data lines
// only, header is prepended).
func tree(t *testing.T, pin string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(root, filepath.FromSlash(DefaultPinFile))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(pinHeader+pin), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func run(t *testing.T, root string, dirs ...string) (Outcome, error) {
	t.Helper()
	if len(dirs) == 0 {
		dirs = []string{"infra/tests"}
	}
	return Check(Config{RepoRoot: root, ScanDirs: dirs, PinFile: DefaultPinFile})
}

// A file with one honest bare skip (nothing under it). Every fixture includes
// it so the third and fourth floors are satisfied and the test under way is the
// only thing that can fail.
const honest = `package t

import "testing"

func TestHonest(t *testing.T) {
	t.Skip("requires a cloud cluster")
}
`

func joinViolations(o Outcome) string { return strings.Join(o.Violations, "\n") }

// --- the shape --------------------------------------------------------------

func TestUnconditionalSkipWithCodeBelowIsAViolation(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/honest_test.go": honest,
		"infra/tests/dead_test.go": `package t

import "testing"

func TestDead(t *testing.T) {
	t.Skip("contract moved elsewhere")
	if 1 != 1 {
		t.Error("never runs")
	}
}
`,
	})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if o.Pass {
		t.Fatal("a bare skip with an assertion under it must FAIL")
	}
	v := joinViolations(o)
	for _, want := range []string{"dead_test.go", "TestDead", "1 statement(s) follow"} {
		if !strings.Contains(v, want) {
			t.Errorf("violation must name %q; got:\n%s", want, v)
		}
	}
}

func TestBareSkipWithNothingBelowIsNotAViolation(t *testing.T) {
	root := tree(t, "", map[string]string{"infra/tests/honest_test.go": honest})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if !o.Pass {
		t.Fatalf("a skip that aborts a test with nothing under it is honest; got %v", o.Violations)
	}
	if len(o.Unconditional) != 1 {
		t.Errorf("want 1 unconditional skip counted, got %d", len(o.Unconditional))
	}
	if len(o.Sites) != 0 {
		t.Errorf("want 0 sites, got %v", o.Sites)
	}
}

// A skip inside an `if` is CONDITIONAL in form. Whether it is unconditional in
// FACT is test-skip-absent-path's question, and this gate must not answer it --
// two gates claiming the same site would double-report every guarded skip.
func TestGuardedSkipIsNotThisGatesSubject(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/honest_test.go": honest,
		"infra/tests/guarded_test.go": `package t

import (
	"os"
	"testing"
)

func TestGuarded(t *testing.T) {
	if _, err := os.Stat("../../cli/src"); os.IsNotExist(err) {
		t.Skip("cli/ moved to a sibling repo")
	}
	t.Error("this is reachable when the path is present")
}
`,
	})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if !o.Pass {
		t.Fatalf("a guarded skip is the sibling gate's subject; got %v", o.Violations)
	}
	if len(o.Sites) != 0 {
		t.Errorf("a guarded skip must not be a site here; got %v", o.Sites)
	}
}

// Six of this item's eight sites sat in t.Run closures, so the literal position
// is not optional coverage.
func TestSkipInsideFunctionLiteralIsASite(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/honest_test.go": honest,
		"infra/tests/sub_test.go": `package t

import "testing"

func TestOuter(t *testing.T) {
	t.Run("legacy flow", func(t *testing.T) {
		t.Skip("flow removed at the cutover")
		got := 1
		if got != 2 {
			t.Error("never runs")
		}
	})
}
`,
	})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if o.Pass {
		t.Fatal("a bare skip in a t.Run closure with code under it must FAIL")
	}
	if !strings.Contains(joinViolations(o), "func literal") {
		t.Errorf("violation must say the skip sits in a function literal; got:\n%s", joinViolations(o))
	}
}

// --- the pin ----------------------------------------------------------------

func TestDeclaredSiteIsTrackedAndPasses(t *testing.T) {
	root := tree(t,
		"infra/tests/dead_test.go\tTestDead\tThe trailing return is the tail Go requires and asserts nothing at all.\n",
		map[string]string{
			"infra/tests/honest_test.go": honest,
			"infra/tests/dead_test.go": `package t

import "testing"

func TestDead(t *testing.T) {
	t.Skip("declared")
	_ = 1
}
`,
		})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if !o.Pass {
		t.Fatalf("a declared site must pass; got %v", o.Violations)
	}
	if len(o.Tracked) != 1 {
		t.Fatalf("want 1 tracked site, got %v", o.Tracked)
	}
	// Printed on EVERY run: a declaration nobody sees is the dead assertion
	// with extra steps.
	if !strings.Contains(o.Tracked[0], "TestDead") {
		t.Errorf("tracked line must name the function; got %q", o.Tracked[0])
	}
}

// THE FLOOR THIS GATE EXISTS TO KEEP. The pin's anchor is the whole shape, so a
// declaration the scan cannot re-find is either stale prose or a detector that
// stopped detecting. Both are red.
func TestPinLineNotReFoundFails(t *testing.T) {
	root := tree(t,
		"infra/tests/gone_test.go\tTestGone\tDeclared once, and the site it named is no longer there to find.\n",
		map[string]string{"infra/tests/honest_test.go": honest})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if o.Pass {
		t.Fatal("a pin line nobody re-found must FAIL, not pass quietly")
	}
	v := joinViolations(o)
	for _, want := range []string{"TestGone", "gone_test.go", "did not re-find"} {
		if !strings.Contains(v, want) {
			t.Errorf("violation must name %q; got:\n%s", want, v)
		}
	}
}

// A declared site whose dead code was removed is ALSO a stale pin line: the
// skip is still there, but it is no longer a site, so the anchor is gone.
func TestPinLineForFixedSiteFails(t *testing.T) {
	root := tree(t,
		"infra/tests/fixed_test.go\tTestFixed\tDeclared while it still carried dead code underneath the skip.\n",
		map[string]string{
			"infra/tests/honest_test.go": honest,
			"infra/tests/fixed_test.go": `package t

import "testing"

func TestFixed(t *testing.T) {
	t.Skip("the dead assertions under this were removed")
}
`,
		})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if o.Pass {
		t.Fatal("a pin line for a site that is no longer a site must FAIL")
	}
	if !strings.Contains(joinViolations(o), "TestFixed") {
		t.Errorf("violation must name TestFixed; got:\n%s", joinViolations(o))
	}
}

func TestPinCannotAnswerForTwoSitesInOneFunction(t *testing.T) {
	root := tree(t,
		"infra/tests/two_test.go\tTestTwo\tOne declaration cannot answer for two sites without forgiving one.\n",
		map[string]string{
			"infra/tests/honest_test.go": honest,
			"infra/tests/two_test.go": `package t

import "testing"

func TestTwo(t *testing.T) {
	t.Run("a", func(t *testing.T) {
		t.Skip("one")
		_ = 1
	})
	t.Run("b", func(t *testing.T) {
		t.Skip("two")
		_ = 2
	})
}
`,
		})
	_, err := run(t, root)
	if err == nil {
		t.Fatal("an ambiguous declaration must be a SETUP error, not a silent forgiveness")
	}
	if !strings.Contains(err.Error(), "TestTwo") {
		t.Errorf("error must name the function; got %v", err)
	}
}

func TestMalformedPinIsSetupBroken(t *testing.T) {
	cases := []struct{ name, pin, want string }{
		{"wrong field count", "infra/tests/a_test.go\tTestA\n", "3 TAB-separated"},
		{"reason too short", "infra/tests/a_test.go\tTestA\ttoo short\n", "characters"},
		{"empty func", "infra/tests/a_test.go\t\tA reason long enough to clear the minimum length bar.\n", "must not be empty"},
		{"duplicate", "infra/tests/a_test.go\tTestA\tA reason long enough to clear the minimum length bar.\n" +
			"infra/tests/a_test.go\tTestA\tAnother reason long enough to clear the minimum length bar.\n", "duplicate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := tree(t, c.pin, map[string]string{"infra/tests/honest_test.go": honest})
			_, err := run(t, root)
			if err == nil {
				t.Fatal("a pin the gate cannot read is a BROKEN SETUP, never a pass")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error must explain %q; got %v", c.want, err)
			}
		})
	}
}

func TestUnreadablePinIsSetupBroken(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "infra", "tests", "honest_test.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(honest), 0o644); err != nil {
		t.Fatal(err)
	}
	// no pin file at all
	_, err := run(t, root)
	if err == nil {
		t.Fatal("a missing pin file must be a BROKEN SETUP, never a pass")
	}
}

// --- the floors -------------------------------------------------------------

func TestMissingScanDirIsSetupBroken(t *testing.T) {
	root := tree(t, "", map[string]string{"infra/tests/honest_test.go": honest})
	_, err := run(t, root, "infra/tests", "api")
	if err == nil {
		t.Fatal("a scan directory that is not there must be an error, not a pass")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error must name the missing root; got %v", err)
	}
}

// Counted PER ROOT, so a populous tree cannot cover for a root that drifted.
func TestScanRootWithNoTestFilesIsSetupBroken(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/honest_test.go": honest,
		"api/README.md":              "not a test file\n",
	})
	_, err := run(t, root, "infra/tests", "api")
	if err == nil {
		t.Fatal("a scan root with no *_test.go must be an error, not a pass")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error must name the empty root; got %v", err)
	}
}

func TestNoSkipCallAtAllIsSetupBroken(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/plain_test.go": `package t

import "testing"

func TestPlain(t *testing.T) {
	if 1 != 1 {
		t.Error("no")
	}
}
`,
	})
	_, err := run(t, root)
	if err == nil {
		t.Fatal("zero skip calls must be a BROKEN SETUP: a dead detector looks exactly like a clean tree")
	}
	if !strings.Contains(err.Error(), "no skip call") {
		t.Errorf("error must say the detector found no skips; got %v", err)
	}
}

// THE FLOOR ABOVE IS NOT ENOUGH, and this is why it has a sibling: this gate
// matches a strictly narrower shape than "a skip call", so a working skip
// counter proves nothing about the function-body-level test on top of it.
func TestSkipsButNoUnconditionalOneIsSetupBroken(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/guarded_test.go": `package t

import (
	"os"
	"testing"
)

func TestGuarded(t *testing.T) {
	if _, err := os.Stat("../../cli/src"); os.IsNotExist(err) {
		t.Skip("guarded, so not unconditional")
	}
}
`,
	})
	o, err := run(t, root)
	if err == nil {
		t.Fatalf("skips present but none unconditional must be a BROKEN SETUP; got pass=%v", o.Pass)
	}
	if !strings.Contains(err.Error(), "UNCONDITIONAL") {
		t.Errorf("error must name the narrower shape it could not find; got %v", err)
	}
}

// --- accounting -------------------------------------------------------------

func TestSkipCallsCountsEverySpelling(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/spellings_test.go": `package t

import "testing"

func TestSpellings(t *testing.T) {
	if false {
		t.Skipf("formatted %s", "skip")
	}
	if false {
		t.SkipNow()
	}
	t.Skip("plain")
}
`,
	})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if o.SkipCalls != 3 {
		t.Errorf("want 3 skip calls (Skip, Skipf, SkipNow), got %d", o.SkipCalls)
	}
	if len(o.Unconditional) != 1 {
		t.Errorf("want 1 unconditional (the guarded two do not count), got %d", len(o.Unconditional))
	}
}

// The reason is quoted back so a reader can decide whether the assertion should
// come back or go WITHOUT opening the file.
func TestViolationQuotesTheSkipReason(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/honest_test.go": honest,
		"infra/tests/dead_test.go": `package t

import "testing"

func TestDead(t *testing.T) {
	t.Skip("monolith main.go deleted T1.4")
	_ = 1
}
`,
	})
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if !strings.Contains(joinViolations(o), "monolith main.go deleted T1.4") {
		t.Errorf("violation must quote the skip's own reason; got:\n%s", joinViolations(o))
	}
}

// A skip reached through a field is still a skip. The copy of isSkipCall this
// gate kept required a bare identifier receiver, so `h.t.Skip()` over dead
// assertions was invisible here while the sibling gate already counted it.
func TestSkipThroughAFieldWithCodeBelowIsAViolation(t *testing.T) {
	root := tree(t, "", map[string]string{
		"infra/tests/honest_test.go": honest,
		"infra/tests/field_test.go": `package t

import "testing"

type harness struct{ t *testing.T }

func TestField(t *testing.T) {
	h := harness{t: t}
	h.t.Skip("moved")
	h.t.Error("never runs")
}
`,
	})

	o, err := run(t, root)

	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if !strings.Contains(joinViolations(o), "TestField") {
		t.Fatalf("h.t.Skip() with a statement below must be a violation; got:\n%s", joinViolations(o))
	}
}

// The stale-pin message must name the pin file the gate actually read.
func TestStalePinViolationNamesTheConfiguredPinFile(t *testing.T) {
	root := tree(t, "", map[string]string{"infra/tests/honest_test.go": honest})
	custom := "infra/quality/custom-pin.txt"
	if err := os.WriteFile(filepath.Join(root, custom), []byte(pinHeader+"infra/tests/gone_test.go\tTestGone\tthe contract moved to the api integration suite, which asserts the same invariant\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	o, err := Check(Config{RepoRoot: root, ScanDirs: []string{"infra/tests"}, PinFile: custom})

	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	if !strings.Contains(joinViolations(o), custom) {
		t.Fatalf("stale-pin violation must name %s; got:\n%s", custom, joinViolations(o))
	}
}
