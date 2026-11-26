// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/skipshape"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// DefaultScanDirs are the trees this gate governs; see skipshape.
var DefaultScanDirs = skipshape.DefaultScanDirs

// DefaultPinFile is the declaration of tracked path-guarded skips. It sits in
// infra/quality/ beside the other pinned contracts this repo keeps for its own
// test governance (test-pyramid-baseline.json, skip-link-baseline.txt).
const DefaultPinFile = "infra/quality/test-skip-absent-path.txt"

// maxResolveDepth bounds the expression resolver against a cyclic binding, and
// bounds how far a guard's answer is followed through predicates that delegate
// to other predicates (B-186). Past it the guard is UNRESOLVED and the gate
// FAILS -- the bound costs a verdict, never a silence.
const maxResolveDepth = 24

// rootAnchor marks a resolved path as already rooted at the repository root,
// as opposed to relative to the test's package directory. A path built from
// runtime.Caller carries it; a bare "../../foo" literal does not, and the two
// must not be measured from the same place. It is deliberately not path-shaped
// so filepath.Join and filepath.Clean cannot eat it.
const rootAnchor = "__LOOP_RUNTIME_REPO_ROOT__"

// Config drives the gate.
type Config struct {
	// RepoRoot is this repository's root. Defaults to ".".
	RepoRoot string
	// ScanDirs overrides DefaultScanDirs (repo-relative).
	ScanDirs []string
	// PinFile overrides DefaultPinFile (repo-relative).
	PinFile string
}

// Verdict is what the gate concluded about one guard's path.
type Verdict int

const (
	// Unresolved: the gate could not turn the guard's expression into a path.
	Unresolved Verdict = iota
	// External: the path is rooted outside this repository.
	External
	// Present: the path exists in this tree, so the guard is conditional.
	Present
	// Absent: the path is missing, so the skip always fires.
	Absent
	// Outside: the path resolved, but to a place this repository does not
	// contain -- an absolute literal, or a climb above the repository root.
	// Reported with the others under EXTERNAL, and never joined onto the repo
	// root: the fabricated path was the whole defect (B-183).
	Outside
)

// Site is one detected path-guarded skip.
type Site struct {
	// File is the test file, repo-relative with forward slashes.
	File string
	// Func is the enclosing function declaration's name. Together with File
	// and Path it is the pin key -- stable under edits that move lines.
	Func string
	// Line is where the skip call sits, for the human reading the output.
	Line int
	// Expr is the guard's path expression as written, for messages.
	Expr string
	// Path is the resolved repo-relative path. Empty unless Verdict is
	// Present, Absent or Outside -- and for Outside it is the path AS WRITTEN,
	// which is the point: nothing is joined onto a root it does not share.
	Path string
	// Verdict is the gate's conclusion about Path.
	Verdict Verdict
}

// Key is the pin key for this site.
func (s Site) Key() string { return s.File + "::" + s.Func + "::" + s.Path }

// Pin is one declared, tracked path-guarded skip.
type Pin struct {
	File   string
	Func   string
	Path   string
	Reason string
	Line   int
}

// Key is the site key this pin claims to cover.
func (p Pin) Key() string { return p.File + "::" + p.Func + "::" + p.Path }

// Outcome is the verdict.
type Outcome struct {
	Pass bool

	// ScanRoots are the directories that were walked, as resolved.
	ScanRoots []string
	// FilesScanned and SkipCalls are the floors' measurements, reported so a
	// reader can see the gate had a subject.
	FilesScanned int
	SkipCalls    int

	// Sites is every path-guarded skip found, sorted by file then line.
	Sites []Site
	// Conditional, Tracked and Externals are the non-failing classifications,
	// each printed on every run.
	Conditional []string
	Tracked     []string
	Externals   []string
	// Violations is what makes the gate fail.
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// The anti-vacuity floors. Each is a BROKEN SETUP, not a clean tree.
var (
	errNoTestFiles = errors.New("found no *_test.go file -- a scan over an empty population reports PASS while asserting nothing")
	errNoSkipCalls = errors.New("found no skip call at all -- either every test in the tree lost its skip, or this gate's AST detector no longer matches the shape of one, and a silent zero here turns the whole gate into a no-op")
)

// fsProbes are the calls whose error result reports whether a path exists,
// mapped to the index of their path argument. Open/OpenFile are here because a
// test that opens-and-skips guards on existence exactly as one that
// stats-and-skips does.
var fsProbes = map[string]map[string]int{
	"os": {
		"Stat":     0,
		"Lstat":    0,
		"Open":     0,
		"OpenFile": 0,
		"ReadFile": 0,
		"ReadDir":  0,
	},
	"ioutil": {
		"ReadFile": 0,
		"ReadDir":  0,
	},
}

// Check runs the gate. It returns an error only for a broken SETUP (a missing
// scan tree, an unreadable or malformed pin, a detector that found nothing);
// a violated contract comes back as Outcome.Violations with Pass false.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	scanDirs := cfg.ScanDirs
	if len(scanDirs) == 0 {
		scanDirs = DefaultScanDirs
	}
	pinFile := cfg.PinFile
	if pinFile == "" {
		pinFile = DefaultPinFile
	}

	pins, err := parsePins(filepath.Join(root, pinFile))
	if err != nil {
		return nil, err
	}

	out := &Outcome{}
	for _, dir := range scanDirs {
		scanRoot := filepath.Join(root, dir)
		info, err := os.Stat(scanRoot)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("the scan tree %s is not a directory (%v) -- this gate has no subject, "+
				"which is a broken setup and not a clean repository", scanRoot, err)
		}
		out.ScanRoots = append(out.ScanRoots, scanRoot)
		// The file floor is PER ROOT: a configured tree with no test file in it
		// is a root that drifted, and averaging it away behind the other root's
		// hundreds is how a scan quietly stops covering half its subject.
		before := out.FilesScanned
		if err := scan(out, root, scanRoot); err != nil {
			return nil, err
		}
		if out.FilesScanned == before {
			return nil, fmt.Errorf("%s: %w", scanRoot, errNoTestFiles)
		}
	}
	// The skip floor is GLOBAL: it guards one AST matcher shared by every root,
	// and per-root it would punish the right fix -- deleting a tree's last dead
	// skip would turn the gate red for the deed it exists to encourage.
	if out.SkipCalls == 0 {
		return nil, fmt.Errorf("%s: %w", strings.Join(out.ScanRoots, ", "), errNoSkipCalls)
	}

	classify(out, pins, pinFile)
	out.Pass = len(out.Violations) == 0
	return out, nil
}

// classify joins the scan against the pin and writes the verdict.
func classify(out *Outcome, pins []Pin, pinFile string) {
	byKey := make(map[string]Pin, len(pins))
	for _, p := range pins {
		byKey[p.Key()] = p
	}
	matched := make(map[string]bool, len(pins))

	for _, s := range out.Sites {
		switch s.Verdict {
		case Unresolved:
			out.Violations = append(out.Violations, fmt.Sprintf(
				"%s:%d: %s skips on a filesystem check of `%s`, and this gate could not resolve that "+
					"expression to a path -- so it cannot say whether the skip is conditional or permanent, "+
					"and it will not report clean over what it did not read. Make the path a literal (or a "+
					"literal joined under a helper in the same package)",
				s.File, s.Line, s.Func, s.Expr))
		case External:
			out.Externals = append(out.Externals, fmt.Sprintf(
				"%s:%d %s -> `%s` (rooted outside this repository; NOT judged)",
				s.File, s.Line, s.Func, s.Expr))
		case Outside:
			// Named as written, never rebased onto the repository root. The
			// defect this replaces printed a path that could not exist as
			// spelled, and a fabricated path is worse than a withheld verdict.
			out.Externals = append(out.Externals, fmt.Sprintf(
				"%s:%d %s -> `%s` resolves to %s, which this repository does not contain; NOT judged "+
					"(whether it is on the machine running the suite is not a claim about this tree)",
				s.File, s.Line, s.Func, s.Expr, s.Path))
		case Present:
			if pin, pinned := byKey[s.Key()]; pinned {
				matched[s.Key()] = true
				out.Violations = append(out.Violations, fmt.Sprintf(
					"%s:%d declares %s::%s on %s, but that path EXISTS in this tree -- the guard is "+
						"conditional again and the declaration has become stale prose. Remove the line and "+
						"let the test assert",
					pinFile, pin.Line, s.File, s.Func, s.Path))
				continue
			}
			out.Conditional = append(out.Conditional,
				fmt.Sprintf("%s::%s -> %s", s.File, s.Func, s.Path))
		case Absent:
			pin, pinned := byKey[s.Key()]
			if !pinned {
				out.Violations = append(out.Violations, fmt.Sprintf(
					"%s:%d: %s skips when %s is missing, and that path is ABSENT from this tree -- the skip "+
						"is conditional in form and unconditional in fact, so every assertion below it is "+
						"dead. Remove the test naming the guard that covers the contract instead (B-169), or "+
						"declare it in %s as a TAB-separated line:\n      %s\t%s\t%s\t<why, naming what covers this instead>",
					s.File, s.Line, s.Func, s.Path, pinFile, s.File, s.Func, s.Path))
				continue
			}
			matched[s.Key()] = true
			out.Tracked = append(out.Tracked,
				fmt.Sprintf("%s::%s -> %s (%s)", s.File, s.Func, s.Path, pin.Reason))
		}
	}

	// Every pin must be re-found. This is what stops the gate from dying in
	// silence: if the detector stops detecting, the anchors go unmatched.
	for _, p := range pins {
		if matched[p.Key()] {
			continue
		}
		out.Violations = append(out.Violations, fmt.Sprintf(
			"%s:%d declares a path-guarded skip in %s::%s on %s, and this run's scan did NOT find it. "+
				"Either the skip was removed (delete this line) or this gate stopped recognising it "+
				"(fix the detector) -- a declaration nobody re-finds protects nothing",
			pinFile, p.Line, p.File, p.Func, p.Path))
	}
}
