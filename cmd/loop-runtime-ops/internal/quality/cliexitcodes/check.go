// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package cliexitcodes implements the `cli-exit-codes` gate (migrated from
// infra/tests/scripts/check-cli-exit-codes_test.sh, plan 2026-05-23).
//
// THE CONTRACT. CLAUDE.md Rule 4: a CLI `process.exit()` code is 0-5 only --
// the permanent CLI exit-code contract. 130 (SIGINT) and 143 (SIGTERM) are
// allowed ONLY inside a `process.on('SIG...')` handler (within the three lines
// above, inclusive). Test files (*.test.ts, *.spec.ts, __tests__/) are exempt.
//
// PROBLEM (B-185). Until 2026-09-05 this gate stat'd <repo>/cli/src and, when
// that directory was missing, returned `Outcome{Pass: true, Skipped: true}`;
// the runner printed `SKIP -- cli/src/ not present` and exited 0. But cli/ left
// this repository in 686b9a4c5 (2026-06-16, "remove cli/ -- extracted to
// standalone loop-runtime-cli (M2)") and no loop-runtime-cli checkout sits beside it: measured
// 2026-09-05, no directory named loop-runtime-cli exists anywhere beside this
// repository, and ../loop-runtime-cli does not resolve.
// So the skip branch is the ONLY branch that has ever run here. It did not
// lie in what it printed -- it said
// SKIP -- but the gate still exited 0 and still counted toward the chain's
// green, so the number of gates that RAN overstated the number that CHECKED.
//
// WHAT THIS GATE ANSWERS NOW. It looks for the CLI source in two places, in
// order: <repo>/cli/src (if the CLI ever comes home) and <loop-runtime-cli>/src (the
// standalone checkout, default <repo>/../loop-runtime-cli, overridable). Then exactly
// one of four things, named in the output:
//
//  1. a source tree was found and every exit code is in contract -- PASS, and
//     the output names the tree it walked and how many files it read;
//  2. a source tree was found and some exit code is out of contract -- FAIL,
//     one line per violation;
//  3. no source tree was found and the absence is DECLARED in the pin
//     (infra/quality/cli-exit-codes-subject.txt) -- the absence is printed on
//     EVERY run with the measurement behind it, the verdict is PARTIAL, and
//     the word PASS is never printed. Outcome.Pass() is false: a caller that
//     reads only the boolean cannot mistake this for a check that happened;
//  4. no source tree was found and NOTHING declares that -- FAIL. An absence
//     nobody wrote down is the bug this item was opened for.
//
// WHY A PIN AND NOT A CONSTANT (B-183's property). A declared absence has to
// stop declaring when the subject comes back, and it has to do it loudly. So
// the declaration is a checked-in line, not a string in this file, and the
// gate FAILS when the pin declares an absence the filesystem contradicts:
// "cli/src is declared absent, but <path> exists -- delete the line". That is
// the same property infra/quality/test-skip-absent-path.txt has (B-180/B-183),
// for the same reason: the right way to leave the pin is to delete the line.
//
// The contradiction is looked for in TWO places, and until 2026-09-05 only one
// of them was (B-190). A pinned line is stale if a CLI source tree turned up in
// either candidate, and it is equally stale if the line's own SUBJECT path
// exists -- but the subject field was never stat'd. An audit pinned
//
//	docs	this subject is right here and present
//
// in a root that CONTAINS docs/, and the gate printed "docs is not in this tree
// and not in any checkout beside it" at exit 0: a false sentence about the
// filesystem, from the gate whose reason to exist is not to print those. Every
// subject is stat'd on every run now, and the path that was stat'd is printed
// beside the declaration it produced.
//
// And the fail-on-return branch is not decorative. Searching the sibling is
// what gives it a realistic trigger: cli/ coming HOME to this tree is unlikely
// (M2 extracted it on purpose), but somebody cloning loop-runtime-cli beside this
// repository is an ordinary Tuesday, and on that Tuesday this gate starts
// checking real files and the stale pin line turns red.
//
// WHAT IS NOT COVERED WHILE THE SUBJECT IS ABSENT. Nothing in this repository
// is checked against Rule 4 by this gate -- there is no exit-code call here
// for it to read. The contract holds only wherever loop-runtime-cli's own CI runs,
// which is UNVERIFIED from this tree. That sentence is printed on every run;
// it is not implied by a SKIP.
package cliexitcodes

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// DefaultPinFile is the checked-in absence declaration, repo-relative. It sits
// beside infra/quality/test-skip-absent-path.txt, which pins the same KIND of
// fact for a different subject: a thing this repository looked at, found
// missing, and decided to carry in the open.
const DefaultPinFile = "infra/quality/cli-exit-codes-subject.txt"

// DefaultCLIDir is where the standalone loop-runtime-cli checkout is expected relative
// to the repo root.
const DefaultCLIDir = "../loop-runtime-cli"

// InRepoSubject is the path the CLI occupied while it lived here, and the
// first place the gate looks.
const InRepoSubject = "cli/src"

// Config drives the gate.
type Config struct {
	// RepoRoot is this repository's root. Defaults to ".".
	RepoRoot string
	// CLIRoot is the standalone loop-runtime-cli checkout. Defaults to
	// RepoRoot/../loop-runtime-cli. It may legitimately not exist -- see the package
	// doc.
	CLIRoot string
	// PinFile overrides DefaultPinFile (repo-relative).
	PinFile string
}

// Absence is one pinned line: a subject path this gate would scan, and why it
// is not here.
type Absence struct {
	Subject string
	Why     string
	Line    int
	// ResolvedPath is where Subject was looked for on THIS run, resolved
	// against the repository root. It travels with the absence so the printed
	// declaration carries the measurement that produced it instead of a
	// sentence about a filesystem nobody read (B-190).
	ResolvedPath string
}

// Outcome is the verdict.
//
// Its verdict is Fail on any violation. A run that found no CLI source to read
// is still a Pass: its report says PARTIAL and names what it did not check.
type Outcome struct {
	// Checked is true only when a CLI source tree was found and walked.
	Checked bool
	// ScannedRoot is the directory that was walked; empty when !Checked.
	ScannedRoot string
	// ScannedFiles counts the non-exempt regular files actually read.
	ScannedFiles int
	// SearchedPaths lists every candidate looked at, in order. Never empty.
	SearchedPaths []string

	// TrackedAbsences are the pinned lines whose Subject this run stat'd and
	// did NOT find. A line whose subject EXISTS never reaches this field: it
	// is a violation. Until 2026-09-05 this doc said "the pinned lines that
	// the filesystem confirms" while no subject was ever stat'd -- only the
	// two hardcoded candidates were -- so a line naming a directory sitting
	// right here was reported as an absence (B-190).
	TrackedAbsences []Absence

	Violations []string
}

// Pass reports a check that HAPPENED and found nothing wrong.
//
// It is deliberately a method and not a field. The defect this package was
// rewritten for was one struct literal -- `Outcome{Pass: true, Skipped: true}`
// -- which let "there was nothing to check" be read downstream as "checked and
// fine". Derived from what was read, that literal cannot be written: no caller
// can claim a pass for a scan that did not happen.
//
// `Checked` alone is not that. It becomes true when a directory is located, before
// a file is opened, so a subject that exists and yields nothing scored a pass over
// zero files. An audit walked this gate's own prescribed workflow into that state
// on 2026-09-05: loop-runtime-cli vendored under node_modules with `process.exit(42)` in it,
// the gate FAILs and tells the operator the absence has ended and to delete the pin
// line, the operator does, and the next run prints PASS with the call unread.
//
// Strictly worse than the SKIP this item replaced, which at least said something.
// So a pass requires a file to have been read.
func (o *Outcome) Pass() bool {
	return o.Checked && o.ScannedFiles > 0 && len(o.Violations) == 0
}

var exitRe = regexp.MustCompile(`process\.exit\((\d+)\)`)

// Verdict is Fail when any violation was found and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.Of(o.Violations) }

// Check finds the CLI source tree and enforces the 0-5 exit-code contract on
// it. When no tree is found, the absence must be declared in the pin; when a
// tree IS found, the pin must not still declare it absent.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	cliRoot := cfg.CLIRoot
	if cliRoot == "" {
		cliRoot = filepath.Join(root, DefaultCLIDir)
	}
	pinFile := cfg.PinFile
	if pinFile == "" {
		pinFile = DefaultPinFile
	}
	pinPath := filepath.Join(root, pinFile)

	out := &Outcome{
		SearchedPaths: []string{
			filepath.Join(root, InRepoSubject),
			filepath.Join(cliRoot, "src"),
		},
	}

	pinned, err := parsePin(pinPath)
	if err != nil {
		return nil, err
	}

	// Every pinned SUBJECT is stat'd before anything else is decided. A
	// declaration nobody re-measures is prose (B-190).
	declaredAbsent, contradicted, err := measurePins(root, pinned, pinFile)
	if err != nil {
		return nil, err
	}
	out.Violations = append(out.Violations, contradicted...)

	srcDir := firstExistingDir(out.SearchedPaths)

	if srcDir == "" {
		// `len(declaredAbsent)`, not `len(pinned)`: a pin whose only line names
		// a subject that is right here declares no absence at all, so the
		// absence of the CLI tree is still undeclared and still the bug.
		if len(declaredAbsent) == 0 {
			out.Violations = append(out.Violations, fmt.Sprintf(
				"no CLI source tree was found (looked at %s) and %s declares no absence -- "+
					"an absence nobody wrote down is indistinguishable from a check that ran",
				strings.Join(out.SearchedPaths, ", "), pinFile))
		}
		out.TrackedAbsences = declaredAbsent
		// Checked stays false, so Pass() is false: nothing was scanned, so
		// nothing passed. The runner turns this into PARTIAL, never PASS.
		return out, nil
	}

	out.Checked = true
	out.ScannedRoot = srcDir

	// The measurement is carried whichever branch runs. It was assigned only when
	// no tree was found, so a line this run stat'd and did not find lost its
	// verdict the moment some OTHER tree turned up (B-190's own audit).
	out.TrackedAbsences = declaredAbsent

	for _, a := range declaredAbsent {
		// "this line's subject came back" and "some CLI tree was found" are
		// different facts, and one sentence reported both. A line whose subject
		// is still absent was told its absence had closed -- a false statement
		// about the filesystem, printed by the gate that exists to stop those.
		//
		// A subject that is PRESENT is not in declaredAbsent at all; measurePins
		// puts it in `contradicted`. So every line here is genuinely still
		// absent, and what changed is that the gate now scans anyway.
		out.Violations = append(out.Violations, fmt.Sprintf(
			"%s:%d: %s is still ABSENT -- this run stat'd it and did not find it -- but a "+
				"CLI source tree EXISTS at %s, so this gate SCANS rather than declaring. "+
				"The line no longer describes what happens: delete it, or point it at the "+
				"subject that is actually missing",
			pinFile, a.Line, a.Subject, srcDir))
	}
	// A pin the filesystem contradicts is fixed before this gate scans: whatever
	// it read would be reported under a declaration that is already wrong.
	if len(out.Violations) > 0 {
		return out, nil
	}

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || isExcludedFile(srcDir, path) {
			return nil
		}
		out.ScannedFiles++
		hits, err := scanFile(root, path)
		if err != nil {
			return err
		}
		out.Violations = append(out.Violations, hits...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// measurePins stats the SUBJECT of every pinned line and splits the pin into
// the lines the filesystem still agrees with (absent) and the lines it
// contradicts (contradicted, already worded as violations). It reads the
// filesystem and reports; the caller decides what the report means.
//
// This is the half B-190 found missing: the pin's header promised the gate
// FAILS when "a subject listed here is FOUND", and only the two hardcoded
// candidates were ever stat'd, so the promise held for `cli/src` by accident
// and for nothing else.
//
// A subject that cannot be stat'd at all -- a permission error, not an absence
// -- is returned as an error rather than counted absent. Reporting a clean
// absence over a measurement that failed is the same lie in a quieter voice.
func measurePins(root string, pinned []Absence, pinFile string) (absent []Absence, contradicted []string, err error) {
	for _, a := range pinned {
		a.ResolvedPath = filepath.Join(root, a.Subject)
		_, statErr := os.Stat(a.ResolvedPath)
		switch {
		case statErr == nil:
			contradicted = append(contradicted, fmt.Sprintf(
				"%s:%d: %s is declared ABSENT here, but it EXISTS at %s -- the subject is in this "+
					"tree, so the line is a false statement about the filesystem; delete that line",
				pinFile, a.Line, a.Subject, a.ResolvedPath))
		case os.IsNotExist(statErr):
			absent = append(absent, a)
		default:
			return nil, nil, fmt.Errorf("%s:%d: cannot stat declared-absent subject %s at %s: %w",
				pinFile, a.Line, a.Subject, a.ResolvedPath, statErr)
		}
	}
	return absent, contradicted, nil
}

func firstExistingDir(candidates []string) string {
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

// parsePin reads the absence declaration. A missing pin file is not an error:
// it is the same fact as an empty pin -- nothing is declared absent.
//
// The sibling gate testskipabsentpath makes the OPPOSITE call on the same question,
// and the difference is worth stating because it looks like an inconsistency. There
// a pin line is a live ANCHOR, so losing the file silently drops tracked sites and
// the gate goes quiet about what it was told to watch: SETUP BROKEN, exit 5. Here a
// pin line only ever ADDS a declaration, so without the file every absence is
// undeclared -- which is precisely the condition this gate FAILS on. Losing it can
// only make this gate stricter, never laxer, and a floor would buy nothing.
//
// Written down 2026-09-05 after measuring `--pin <missing>` and reaching for a floor
// before checking which direction the loss pushes.
func parsePin(path string) ([]Absence, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is <root>/<pin>, both operator-supplied.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pin %s: %w", path, err)
	}
	var out []Absence
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for scanner.Scan() {
		n++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || strings.TrimSpace(fields[0]) == "" || strings.TrimSpace(fields[1]) == "" {
			return nil, fmt.Errorf("%s:%d: cannot parse %q -- expected two TAB-separated fields, "+
				"`<subject path>\t<why, naming what covers the contract instead>`", path, n, line)
		}
		out = append(out, Absence{Subject: strings.TrimSpace(fields[0]), Why: strings.TrimSpace(fields[1]), Line: n})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read pin %s: %w", path, err)
	}
	return out, nil
}

func isExcludedFile(root, path string) bool {
	// Matched against the path RELATIVE to the scanned root, never the absolute
	// one. A checkout reached through a node_modules directory -- the ordinary way
	// a CLI is consumed -- or any parent directory whose name contains ".test."
	// excluded every file beneath it, and the gate reported the scan as done. The
	// rule is about what a FILE is, not about where its tree happens to live.
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = "/" + filepath.ToSlash(rel)
	return strings.Contains(rel, ".test.") || strings.Contains(rel, ".spec.") ||
		strings.Contains(rel, "/__tests__/") || strings.Contains(rel, "/node_modules/")
}

// scanFile reads one file and reports out-of-contract exits, naming the path
// relative to the repository root so a violation in the sibling checkout reads
// as `../loop-runtime-cli/src/foo.ts` rather than as a bare basename.
func scanFile(repoRoot, absPath string) ([]string, error) {
	// #nosec G304 -- absPath comes from filepath.Walk of the scanned source tree.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	rel, relErr := filepath.Rel(repoRoot, absPath)
	if relErr != nil {
		rel = absPath
	}
	var hits []string
	for i, line := range lines {
		m := exitRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		code, _ := strconv.Atoi(m[1])
		if code >= 0 && code <= 5 {
			continue
		}
		if (code == 130 || code == 143) && sigHandlerPrecedes(lines, i) {
			continue
		}
		hits = append(hits, fmt.Sprintf("%s:%d: process.exit(%d) outside 0-5 contract", rel, i+1, code))
	}
	return hits, nil
}

// sigHandlerPrecedes reports whether a process.on('SIG...) appears within the 3
// lines above (inclusive of the current line), mirroring the bash gate.
func sigHandlerPrecedes(lines []string, idx int) bool {
	start := idx - 3
	if start < 0 {
		start = 0
	}
	for j := start; j <= idx; j++ {
		if strings.Contains(lines[j], "process.on('SIG") {
			return true
		}
	}
	return false
}
