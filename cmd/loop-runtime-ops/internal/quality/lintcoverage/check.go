// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package lintcoverage measures every Go module, and refuses regression per module.
//
// # The defect, measured
//
// `task lint` (`taskfiles/test.yml:512-531`) ran THREE modules out of EIGHT, in three separate
// `cmds` entries — and Task aborts at the first one that exits != 0, so a finding in `pkg`
// kept `api` from being measured. That is why `B-009` reported *"11 findings, all in
// operators"*: `api` had never been looked at.
//
// Measured on 2026-08-18, module by module:
//
//	pkg  operators  api  operators/api  tools/*(2)        0 each   <- the covered ones
//	infra/scripts                                          4
//	infra/tests                                           93
//	cmd/loop-runtime-ops                                         221       <- the binary of the 130 gates
//
// **318 findings in modules nobody lints**, and the largest is the tool that runs every other
// gate in this repository. `B-121` describes the smaller symptom — aborting at the first — and
// misses the bigger one: even without aborting, the task would look at 3 of 8.
//
// # Why a ratchet, and not "fix the 318"
//
// `task lint` is a line of `CLAUDE.md § Pre-completion checklist`. Covering everything without a
// ratchet takes it from green to 318 findings, and a checklist line nobody can tick stops being
// a checklist — the literal complaint of `B-083`. The lock would be switched off, taking with it
// the coverage it had just gained.
//
// The ratchet makes the 318 visible and prevents the 319th: a module at 0 that gains a finding
// fails, even with 221 frozen beside it.
//
// # `gofmt` is a dimension of its own
//
// 155 files out of format, and `pkg` has 30 of them with **0** `golangci-lint` findings —
// no formatting linter is enabled in `.golangci.yml`. One dimension does not cover the
// other, and they are fixed in different ways: formatting is mechanical, `gosec` is not.
//
// # Execution enters through an injectable seam
//
// The DoD of `B-121` requires *"a test that proves a finding in `pkg` does not keep `api` from
// being linted"* — and such a test cannot depend on actually running `golangci-lint`: it would
// be slow, would depend on the tree, and could not FABRICATE the failure it needs to prove.
//
// The decision (T2, pending ratification, 2026-08-18: lint covers every module) was
// recorded in a meeting note under the gitignored `.claude/` tree, which no clone
// carries and which no longer exists; this package comment is its surviving record.
package lintcoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrRegression means a module got worse than its frozen count.
var ErrRegression = errors.New("regression against the baseline")

// ErrUndeclaredModule means a module has no frozen entry.
var ErrUndeclaredModule = errors.New("module with no entry in the baseline")

// ErrRunnerFailed means a module could not be measured.
//
// It is NOT "zero findings": a module that could not be measured, reported as clean, is the
// green-over-nothing this gate exists to prevent.
var ErrRunnerFailed = errors.New("module could not be measured")

// Runner measures one module. It is the seam (DIP) that makes the policy testable without an
// external process — see the package doc.
type Runner interface {
	Lint(root, module string) (int, error)
	Gofmt(root, module string) (int, error)
}

// Config drives the check.
type Config struct {
	RepoRoot     string
	BaselinePath string
	// Runner defaults to the real golangci-lint / gofmt invocation.
	Runner Runner
}

// Failure is one module that could not be measured.
type Failure struct {
	Module string
	Err    string
}

// Outcome reports what was established.
type Outcome struct {
	Modules      []string
	Lint         map[string]int
	Gofmt        map[string]int
	Regressions  []Regression
	Improvements []Improvement
	Undeclared   []string
	Failed       []Failure
	Reason       string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// Measure runs every module and returns the counts, WITHOUT judging them.
//
// Separate from Check on purpose (SRP), and the reason is concrete: `--write` needs to measure to
// be able to freeze for the first time, and `Check` fails on a missing baseline before measuring
// anything. The first version of this slice conflated the two responsibilities and made the
// first freeze impossible.
func Measure(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	runner := cfg.Runner
	if runner == nil {
		runner = execRunner{}
	}

	mods, err := DiscoverModules(root)
	if err != nil {
		return out, err
	}
	out.Modules = mods

	// ALL modules are measured, even when one fails. This is the B-121 bullet: aborting at the
	// first is what kept 5 modules and 318 findings invisible.
	out.Lint = map[string]int{}
	out.Gofmt = map[string]int{}
	for _, m := range mods {
		n, lerr := runner.Lint(root, m)
		if lerr != nil {
			out.Failed = append(out.Failed, Failure{Module: m, Err: lerr.Error()})
		} else {
			out.Lint[m] = n
		}
		g, gerr := runner.Gofmt(root, m)
		if gerr != nil {
			out.Failed = append(out.Failed, Failure{Module: m, Err: gerr.Error()})
		} else {
			out.Gofmt[m] = g
		}
	}
	out.Reason = summarize(mods, out.Lint, out.Gofmt)
	return out, nil
}

// Refrozen reports a baseline rewritten from what was measured.
type Refrozen struct {
	// Reason is the per-module summary of the counts that were frozen.
	Reason string
}

// Verdict is Pass: refreezing is an operation, and a Refrozen exists only when
// the baseline was written. A refreeze that could not complete is an error.
func (Refrozen) Verdict() verdict.Verdict { return verdict.Pass }

// Refreeze measures every module and rewrites the baseline with the counts.
//
// It uses what was MEASURED, even after a regression — refreezing is a
// deliberate act, and the baseline file's diff is where it becomes visible. A
// run that measured nothing has nothing to freeze and is refused.
func Refreeze(cfg Config) (Refrozen, error) {
	out, err := Measure(cfg)
	if err != nil {
		return Refrozen{}, err
	}
	if len(out.Lint) == 0 && len(out.Gofmt) == 0 {
		return Refrozen{}, errors.New("nothing measured, so nothing to freeze")
	}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	path := cfg.BaselinePath
	if path == "" {
		path = filepath.Join(root, BaselineRelPath)
	}
	if err := WriteBaseline(path, out.Lint, out.Gofmt); err != nil {
		return Refrozen{}, err
	}
	return Refrozen{Reason: out.Reason}, nil
}

// Check measures every module and applies the ratchet.
func Check(cfg Config) (Outcome, error) {
	out, err := Measure(cfg)
	if err != nil {
		return out, err
	}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	basePath := cfg.BaselinePath
	if basePath == "" {
		basePath = filepath.Join(root, BaselineRelPath)
	}
	base, err := LoadBaseline(basePath)
	if err != nil {
		return out, err
	}

	vl := base.Compare(out.Lint, base.Lint, "lint")
	vg := base.Compare(out.Gofmt, base.Gofmt, "gofmt")
	out.Regressions = append(vl.Regressions, vg.Regressions...)
	out.Improvements = append(vl.Improvements, vg.Improvements...)
	out.Undeclared = dedup(append(vl.Undeclared, vg.Undeclared...))

	if len(out.Failed) > 0 {
		return out, fmt.Errorf("%w: %s", ErrRunnerFailed, out.Failed[0].Module+": "+out.Failed[0].Err)
	}
	if len(out.Regressions) > 0 {
		r := out.Regressions[0]
		out.Violation = fmt.Errorf("%w: %s [%s] %d → %d — the baseline refuses regression; fix it, or refreeze with `--write` explaining why",
			ErrRegression, r.Module, r.Dimension, r.Was, r.Now)
		return out, nil
	}
	if len(out.Undeclared) > 0 {
		out.Violation = fmt.Errorf("%w (%d): %s — a new module enters DECLARED, never silently; run `--write`",
			ErrUndeclaredModule, len(out.Undeclared), strings.Join(out.Undeclared, ", "))
		return out, nil
	}
	return out, nil
}

// summarize renders the per-module counts — the second bullet of B-121.
func summarize(mods []string, lint, gofmtCounts map[string]int) string {
	parts := make([]string, 0, len(mods))
	tl, tg := 0, 0
	for _, m := range mods {
		parts = append(parts, fmt.Sprintf("%s=%d/%d", m, lint[m], gofmtCounts[m]))
		tl += lint[m]
		tg += gofmtCounts[m]
	}
	return fmt.Sprintf("%d module(s) measured [lint/gofmt]: %s — total %d findings, %d files out of format",
		len(mods), strings.Join(parts, " "), tl, tg)
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// findingRE matches one golangci-lint finding line (`path/file.go:12:3: message`).
var findingRE = regexp.MustCompile(`(?m)^[a-zA-Z0-9_./-]+\.go:[0-9]+:[0-9]+:`)

// execRunner invokes the real tools.
type execRunner struct{}

// lintCacheDir is the golangci-lint cache this module's run may use: one
// directory per module directory, derived rather than shared.
//
// The shared default cache replays the analysis of trees that no longer exist. On
// 2026-09-05 `golangci-lint run ./...` in api/ reported 35 findings, all of them
// under /tmp/b059-lint/api/ and /tmp/clone-B-059/api/ -- neither on disk, neither a
// registered worktree, both debris from an earlier lane. The same run with a
// private cache reported 0. The gate faithfully turned that into `api [lint] 0 ->
// 35` and failed the chain on a module whose own lint is clean.
//
// A false FAIL costs what a false PASS costs. It teaches a reader that the chain's
// red means nothing in particular, and the next real regression is read as more of
// the same.
//
// Keyed by path hash rather than isolated per run, so caching still works: the same
// tree reuses its own cache, and no tree can answer for another.
func lintCacheDir(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(os.TempDir(), "loop-runtime-golangci-"+hex.EncodeToString(sum[:8]))
}

// lintCommand builds the invocation, separated from running it so the environment
// it carries can be asserted without golangci-lint on the box.
func lintCommand(dir string) *exec.Cmd {
	// #nosec G204 -- `dir` derives from the go.mod sweep under the root; no argument comes from
	// user input.
	cmd := exec.Command("golangci-lint", "run", "--timeout", "5m", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOLANGCI_LINT_CACHE="+lintCacheDir(dir))
	return cmd
}

func (execRunner) Lint(root, mod string) (int, error) {
	dir := ModuleDir(root, mod)
	cmd := lintCommand(dir)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// A missing or unrunnable binary is NOT "zero findings".
			return 0, fmt.Errorf("golangci-lint in %s: %w", mod, err)
		}
		code = exitErr.ExitCode()
	}
	n, lerr := lintFindings(code, out)
	if lerr != nil {
		return 0, fmt.Errorf("golangci-lint in %s: %w", mod, lerr)
	}
	return n, nil
}

// lintIssuesExitCode is golangci-lint's exit code for "issues were found".
// Every other non-zero code is a run that did not complete.
const lintIssuesExitCode = 1

// lintFindings turns a golangci-lint exit code and output into a count. A
// crashed run (config error, type-check failure, timeout) prints no finding
// lines; counting it as zero would report an unmeasured module as clean.
func lintFindings(exitCode int, out []byte) (int, error) {
	n := len(findingRE.FindAll(out, -1))
	switch {
	case exitCode == 0:
		return n, nil
	case exitCode == lintIssuesExitCode && n > 0:
		return n, nil
	default:
		return 0, fmt.Errorf("exit %d with %d parseable finding(s) — the run did not complete: %s",
			exitCode, n, lastLine(out))
	}
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines[len(lines)-1]
}

func (execRunner) Gofmt(root, mod string) (int, error) {
	dir := ModuleDir(root, mod)
	// #nosec G204 -- same justification as Lint above.
	cmd := exec.Command("gofmt", "-l", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("gofmt in %s: %w", mod, err)
	}
	n := 0
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, "/vendor/") {
			n++
		}
	}
	return n, nil
}
