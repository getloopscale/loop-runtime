// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testskipabsentpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------------

// tree writes a synthetic repository and returns its root. Keys are
// repo-relative paths; values are file contents. Every test builds its own so
// no test can depend on another's leftovers.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// pinHeader is a comment-only pin, to prove comments and blank lines are
// skipped rather than parsed as malformed entries.
const pinHeader = "# declared path-guarded skips\n\n"

// guardedTest renders a test file whose skip is guarded by os.Stat on path.
func guardedTest(pkg, funcName, path string) string {
	return "package " + pkg + `

import (
	"os"
	"testing"
)

func ` + funcName + `(t *testing.T) {
	if _, err := os.Stat("` + path + `"); os.IsNotExist(err) {
		t.Skipf("absent")
	}
	t.Log("assertion that only runs when the path is here")
}
`
}

func run(t *testing.T, root string) (*Outcome, error) {
	t.Helper()
	return Check(Config{RepoRoot: root, ScanDir: "infra/tests", PinFile: "infra/quality/pin.txt"})
}

func mustRun(t *testing.T, root string) *Outcome {
	t.Helper()
	o, err := run(t, root)
	if err != nil {
		t.Fatalf("Check returned a setup error it should not have: %v", err)
	}
	return o
}

// joined renders every violation as one blob for substring assertions.
func joined(v []string) string { return strings.Join(v, "\n") }

// ---------------------------------------------------------------------------
// the four verdicts
// ---------------------------------------------------------------------------

func TestAbsentAndUndeclaredFails(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader,
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded",
			"../../../cli/src/index.ts"),
	})

	o := mustRun(t, root)

	if o.Pass {
		t.Fatal("a skip guarded by a path that is not in the tree must FAIL: it fires on every run, " +
			"so every assertion below it is dead")
	}
	v := joined(o.Violations)
	for _, want := range []string{
		"infra/tests/unit/a_test.go",
		"TestGuarded",
		"cli/src/index.ts",
		"ABSENT",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("the violation must name %q so the reader can act on it; got:\n%s", want, v)
		}
	}
	if len(o.Tracked) != 0 || len(o.Conditional) != 0 {
		t.Errorf("an undeclared absence is neither tracked nor conditional; tracked=%v conditional=%v",
			o.Tracked, o.Conditional)
	}
}

func TestAbsentAndDeclaredIsTrackedNotSilent(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader +
			"infra/tests/unit/a_test.go\tTestGuarded\tcli/src/index.ts\tmoved to loopzero-cli; the seam is guarded by api/internal/contract/cli_consumer_test.go\n",
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded",
			"../../../cli/src/index.ts"),
	})

	o := mustRun(t, root)

	if !o.Pass {
		t.Fatalf("a declared absence must not fail the gate; violations:\n%s", joined(o.Violations))
	}
	if len(o.Tracked) != 1 {
		t.Fatalf("want exactly 1 tracked absence, got %d: %v", len(o.Tracked), o.Tracked)
	}
	// The REASON has to reach the operator's screen: a declaration nobody reads
	// is the silent skip with extra steps.
	if !strings.Contains(o.Tracked[0], "cli_consumer_test.go") {
		t.Errorf("the tracked line must carry the written reason verbatim, got %q", o.Tracked[0])
	}
	if !strings.Contains(o.Tracked[0], "cli/src/index.ts") {
		t.Errorf("the tracked line must name the guarded path, got %q", o.Tracked[0])
	}
}

func TestPresentPathIsConditionalAndNeedsNoDeclaration(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"docs/thing.md":              "here\n",
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded", "../../../docs/thing.md"),
	})

	o := mustRun(t, root)

	if !o.Pass {
		t.Fatalf("a guard on a path that EXISTS is genuinely conditional and must pass; violations:\n%s",
			joined(o.Violations))
	}
	if len(o.Conditional) != 1 {
		t.Fatalf("want 1 conditional site, got %d: %v", len(o.Conditional), o.Conditional)
	}
	if !strings.Contains(o.Conditional[0], "docs/thing.md") {
		t.Errorf("the conditional line must name the resolved path, got %q", o.Conditional[0])
	}
	if len(o.Tracked) != 0 {
		t.Errorf("nothing is tracked when nothing is absent, got %v", o.Tracked)
	}
}

func TestDeclaredPathThatCameBackFails(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader +
			"infra/tests/unit/a_test.go\tTestGuarded\tdocs/thing.md\twas removed in M5\n",
		"docs/thing.md":              "it is back\n",
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded", "../../../docs/thing.md"),
	})

	o := mustRun(t, root)

	if o.Pass {
		t.Fatal("a declaration whose path came BACK must fail: the guard is conditional again and the " +
			"line has become stale prose")
	}
	v := joined(o.Violations)
	if !strings.Contains(v, "EXISTS in this tree") {
		t.Errorf("the violation must say the path exists, not merely that something is wrong; got:\n%s", v)
	}
	if !strings.Contains(v, "docs/thing.md") {
		t.Errorf("the violation must name the path that returned; got:\n%s", v)
	}
}

func TestExternalPathIsNamedAndNotJudged(t *testing.T) {
	src := `package load

import (
	"os"
	"testing"
)

func TestBackup(t *testing.T) {
	backupDir := os.Getenv("HOME") + "/.loopzero/backups"
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Skipf("backup dir not found: %v", err)
	}
	_ = entries
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/load/a_test.go": src,
	})

	o := mustRun(t, root)

	if !o.Pass {
		t.Fatalf("a guard on a path rooted in the environment is not a claim about this repository "+
			"and must not fail the gate; violations:\n%s", joined(o.Violations))
	}
	if len(o.Externals) != 1 {
		t.Fatalf("want 1 external site, got %d: %v", len(o.Externals), o.Externals)
	}
	// Not judged is not the same as not reported.
	if !strings.Contains(o.Externals[0], "NOT judged") {
		t.Errorf("the external line must say the gate did not judge it, got %q", o.Externals[0])
	}
	if !strings.Contains(o.Externals[0], "TestBackup") {
		t.Errorf("the external line must name the test, got %q", o.Externals[0])
	}
	if len(o.Conditional) != 0 || len(o.Tracked) != 0 {
		t.Errorf("an external path is neither conditional nor tracked; got conditional=%v tracked=%v",
			o.Conditional, o.Tracked)
	}
}

func TestUnresolvablePathFails(t *testing.T) {
	src := `package unit

import (
	"os"
	"testing"
)

func mystery(n int) string { return "/somewhere" }

func TestGuarded(t *testing.T) {
	p := mystery(3)
	if _, err := os.Stat(p); err != nil {
		t.Skip("not here")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)

	if o.Pass {
		t.Fatal("a guard whose path this gate cannot resolve must FAIL: reporting clean over an " +
			"expression it never read is exactly the failure the gate exists to prevent")
	}
	v := joined(o.Violations)
	if !strings.Contains(v, "could not resolve") {
		t.Errorf("the violation must say the gate could not resolve the expression; got:\n%s", v)
	}
	if !strings.Contains(v, "mystery(3)") {
		t.Errorf("the violation must quote the expression it failed on; got:\n%s", v)
	}
}

// ---------------------------------------------------------------------------
// the pin is a set of live anchors
// ---------------------------------------------------------------------------

func TestPinNotRefoundFails(t *testing.T) {
	// The declaration names a skip that this scan does not find. That is either
	// a removed skip or a dead detector, and both need a human.
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader +
			"infra/tests/unit/a_test.go\tTestGone\tcli/src/index.ts\tmoved to loopzero-cli\n",
		"infra/tests/unit/a_test.go": `package unit

import "testing"

func TestStillHere(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
}
`,
	})

	o := mustRun(t, root)

	if o.Pass {
		t.Fatal("a pin line the scan did not re-find must FAIL: the anchor is what proves the detector " +
			"is still detecting")
	}
	v := joined(o.Violations)
	if !strings.Contains(v, "did NOT find it") {
		t.Errorf("the violation must say the scan did not find the declared skip; got:\n%s", v)
	}
	if !strings.Contains(v, "TestGone") {
		t.Errorf("the violation must name the declared func; got:\n%s", v)
	}
}

// ---------------------------------------------------------------------------
// the anti-vacuity floors — each must be a SETUP error, never a pass
// ---------------------------------------------------------------------------

func TestFloorMissingScanTreeIsAnError(t *testing.T) {
	root := tree(t, map[string]string{"infra/quality/pin.txt": pinHeader})

	o, err := run(t, root)
	if err == nil {
		t.Fatalf("a missing infra/tests/ must be an error, not a verdict; got Pass=%v", o.Pass)
	}
	if !strings.Contains(err.Error(), "no subject") {
		t.Errorf("the error must say the gate has no subject; got: %v", err)
	}
}

func TestFloorNoTestFilesIsAnError(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":   pinHeader,
		"infra/tests/README.md":   "no Go here\n",
		"infra/tests/unit/doc.go": "package unit\n",
	})

	o, err := run(t, root)
	if err == nil {
		t.Fatalf("a scan that found no *_test.go must be an error, not a PASS over an empty "+
			"population; got Pass=%v", o.Pass)
	}
	if !strings.Contains(err.Error(), "no *_test.go") {
		t.Errorf("the error must name what was missing; got: %v", err)
	}
}

func TestFloorNoSkipCallsIsAnError(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader,
		"infra/tests/unit/a_test.go": `package unit

import "testing"

func TestNoSkipAnywhere(t *testing.T) { t.Log("hello") }
`,
	})

	o, err := run(t, root)
	if err == nil {
		t.Fatalf("a tree with zero skip calls must be an error: it is far more likely that the "+
			"detector broke than that every skip vanished; got Pass=%v", o.Pass)
	}
	if !strings.Contains(err.Error(), "no skip call") {
		t.Errorf("the error must name what was missing; got: %v", err)
	}
}

func TestFloorMissingPinIsAnError(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded", "../../../nope.txt"),
	})

	o, err := run(t, root)
	if err == nil {
		t.Fatalf("a missing pin must be an error: the pin is committed, so its absence means it was "+
			"deleted or the path drifted; got Pass=%v", o.Pass)
	}
	if !strings.Contains(err.Error(), "broken setup") {
		t.Errorf("the error must call it a broken setup; got: %v", err)
	}
}

func TestFloorMalformedPinIsAnError(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader + "only\ttwo\n",
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded", "../../../nope.txt"),
	})

	if _, err := run(t, root); err == nil {
		t.Fatal("a pin line with fewer than four fields must be an error, not a silently ignored line")
	}
}

func TestFloorPinWithEmptyReasonIsAnError(t *testing.T) {
	// A declaration whose reason is blank declares nothing. The four-field rule
	// is what makes "tracked" mean somebody wrote down why.
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader +
			"infra/tests/unit/a_test.go\tTestGuarded\tnope.txt\t\n",
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded", "../../../nope.txt"),
	})

	if _, err := run(t, root); err == nil {
		t.Fatal("a pin line with an empty reason must be an error: a tracked absence without a written " +
			"reason is a silent skip in a text file")
	}
}

func TestFloorDuplicatePinIsAnError(t *testing.T) {
	line := "infra/tests/unit/a_test.go\tTestGuarded\tnope.txt\treason\n"
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader + line + line,
		"infra/tests/unit/a_test.go": guardedTest("unit", "TestGuarded", "../../../nope.txt"),
	})

	_, err := run(t, root)
	if err == nil {
		t.Fatal("the same skip declared twice must be an error: one skip, one answer")
	}
	if !strings.Contains(err.Error(), "declared twice") {
		t.Errorf("the error must say what is duplicated; got: %v", err)
	}
}

func TestFloorUnparseableGoFileIsAnError(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": "package unit\n\nfunc broken( {\n",
	})

	_, err := run(t, root)
	if err == nil {
		t.Fatal("an unparseable test file must be an error: it is precisely the file whose guards " +
			"would go unread")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("the error must say parsing failed; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// which shapes are and are not a path-guarded skip
// ---------------------------------------------------------------------------

func TestDetectsProbeOnAPrecedingStatement(t *testing.T) {
	// The build-images shape: the probe is its own statement and the guard is
	// the next `if err != nil`.
	src := `package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func readIt(t *testing.T) string {
	_, thisFile, _, _ := runtime.Caller(0)
	wfPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".github", "workflows", "build-images.yml")
	data, err := os.ReadFile(wfPath)
	if err != nil {
		t.Skipf("absent")
	}
	return string(data)
}

func TestUsesIt(t *testing.T) { _ = readIt(t) }
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)

	if len(o.Sites) != 1 {
		t.Fatalf("want 1 detected site, got %d: %+v", len(o.Sites), o.Sites)
	}
	// The runtime.Caller idiom is anchored at the REPO ROOT, not at the package
	// directory. Getting that wrong silently produces a path nobody has.
	if o.Sites[0].Path != ".github/workflows/build-images.yml" {
		t.Errorf("runtime.Caller must anchor at the repo root; got path %q", o.Sites[0].Path)
	}
	if o.Sites[0].Func != "readIt" {
		t.Errorf("the site belongs to the func holding the guard; got %q", o.Sites[0].Func)
	}
}

func TestResolvesThroughAHelperInANonTestFileOfTheSamePackage(t *testing.T) {
	// chartrender's shape: `root, err := repoRoot()` where repoRoot lives in a
	// non-test file. A per-file resolver reports this unresolvable for a reason
	// that is about the gate, not about the tree.
	helper := `package chartrender

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func repoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("nope")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))))
	if _, err := os.Stat(filepath.Join(root, "Taskfile.yml")); err != nil {
		return "", err
	}
	return root, nil
}
`
	test := `package chartrender

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuarded(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	p := filepath.Join(root, "infra", "values", "x.gotmpl")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("not present")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":                  pinHeader,
		"infra/tests/helm/chartrender/h.go":      helper,
		"infra/tests/helm/chartrender/a_test.go": test,
		"infra/values/x.gotmpl":                  "content\n",
	})

	o := mustRun(t, root)

	if len(o.Sites) != 1 {
		t.Fatalf("want 1 detected site, got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Sites[0].Verdict != Present {
		t.Errorf("the helper resolves to the repo root and infra/values/x.gotmpl exists, so the guard "+
			"is CONDITIONAL; got verdict %v for path %q", o.Sites[0].Verdict, o.Sites[0].Path)
	}
	if o.Sites[0].Path != "infra/values/x.gotmpl" {
		t.Errorf("want the resolved path infra/values/x.gotmpl, got %q", o.Sites[0].Path)
	}
}

func TestDetectsErrorsIsGuard(t *testing.T) {
	src := `package unit

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

const p = "../../../gone.txt"

func TestGuarded(t *testing.T) {
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		t.Skip("gone")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("errors.Is(err, fs.ErrNotExist) is a not-exist guard; want 1 site, got %d", len(o.Sites))
	}
	if o.Sites[0].Path != "gone.txt" {
		t.Errorf("a file-level const must resolve; got path %q", o.Sites[0].Path)
	}
}

func TestEnvGatedSkipIsNotASite(t *testing.T) {
	src := `package unit

import (
	"os"
	"testing"
)

func TestLive(t *testing.T) {
	if os.Getenv("LOOPZERO_REGRESSION_LIVE") == "" {
		t.Skip("LOOPZERO_REGRESSION_LIVE not set")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 0 {
		t.Fatalf("an env-gated skip is honestly conditional on the environment, not on a path; "+
			"got %d site(s): %+v", len(o.Sites), o.Sites)
	}
	if o.SkipCalls != 1 {
		t.Errorf("it is still a skip call for the floor's purposes; got %d", o.SkipCalls)
	}
	if !o.Pass {
		t.Errorf("nothing here should fail; violations:\n%s", joined(o.Violations))
	}
}

func TestSkipWhenPathIsPresentIsNotASite(t *testing.T) {
	// The inverted guard: skip because the path IS there. It cannot be an
	// always-fires skip caused by a missing path, so it is out of scope.
	src := `package unit

import (
	"os"
	"testing"
)

func TestGuarded(t *testing.T) {
	if _, err := os.Stat("../../../gone.txt"); err == nil {
		t.Skip("already migrated")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 0 {
		t.Fatalf("`err == nil` is the opposite guard and must not be reported; got %+v", o.Sites)
	}
}

func TestNonSkippingGuardIsNotASite(t *testing.T) {
	// operator_cutover's T-01 shape: stat a path, and on absence try a second
	// path instead of skipping. Flagging it would make the gate cry wolf.
	src := `package unit

import (
	"os"
	"testing"
)

func TestGuarded(t *testing.T) {
	p := "../../../gone.txt"
	if _, err := os.Stat(p); os.IsNotExist(err) {
		p = "../../../also-gone.txt"
	}
	_ = p
	if testing.Short() {
		t.Skip("short")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 0 {
		t.Fatalf("a path guard whose body does not skip is not a path-guarded skip; got %+v", o.Sites)
	}
}

// ---------------------------------------------------------------------------
// the real tree — B-180's own regression
// ---------------------------------------------------------------------------

// repoRootForTest walks up to the umbrella root from this package.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	// cmd/loopzero-ops/internal/quality/testskipabsentpath -> five levels up.
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Taskfile.yml")); err != nil {
		t.Fatalf("expected the repo root at %s (no Taskfile.yml): %v", root, err)
	}
	return root
}

// TestRealTreeIsClean is the regression for B-180 itself: the four cli/ skips
// are gone and the remaining path-guarded skips are declared.
func TestRealTreeIsClean(t *testing.T) {
	root := repoRootForTest(t)
	o, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("the gate must have a subject in this repository: %v", err)
	}
	if !o.Pass {
		t.Fatalf("the tree must be clean; violations:\n%s", joined(o.Violations))
	}
	// The floors must be visibly satisfied, not merely not-failed.
	if o.FilesScanned < 100 {
		t.Errorf("expected the scan to reach the whole infra/tests tree; got %d files", o.FilesScanned)
	}
	if o.SkipCalls < 10 {
		t.Errorf("expected the detector to see the tree's skip calls; got %d", o.SkipCalls)
	}
	// And the four B-180 removed must not come back by any route.
	for _, s := range o.Sites {
		if strings.HasPrefix(s.Path, "cli/") {
			t.Errorf("a guard on %s is back at %s:%d (%s) — those four were removed because this "+
				"repository does not contain cli/ and loopzero-cli is not cloned beside it",
				s.Path, s.File, s.Line, s.Func)
		}
	}
}
