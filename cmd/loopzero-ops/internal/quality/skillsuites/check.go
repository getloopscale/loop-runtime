// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package skillsuites runs the ecosystem's own pytest suites, because nothing else did.
//
// # The finding
//
// `.claude/skills/*/tests/` holds 15 suites guarding the gates that guard everything
// else. They run under no task: `task test:unit` walks the Go modules, `task
// quality:gates` runs the Go gate binary, and neither knows pytest exists. Measured
// 2026-08-16 (B-124): **three of the fifteen were red**, and two had been red long
// enough that the defect they caught had shipped —
//
//   - `discover-plan-confidence` — the "good" fixture scored 3 of 3 targets fabricated,
//     the same layout defect `B-004` had just fixed in the sibling skill;
//   - `code-quality` — the project's languages config was in a format the parser
//     rejects, so `/code-quality` audited ZERO languages and returned `PASS` with
//     `score_cap: 100`, while `cycle-implement.md` treats it as a hard gate;
//   - `plan-confidence` — a kit-internal snapshot test asserting a property of the
//     KIT'S plan corpus, failing on 26 plans belonging to this project.
//
// A suite nobody runs is not a safety net; it is a record of an intention.
//
// # Why a Go gate around pytest
//
// The repository's rule is that new operational logic lives in a quality gate rather
// than a new shell script (`rules/shell-scripts-allowlist.md`, D3). This gate is the
// thinnest thing that satisfies it: it discovers the suites, runs each one, and reports
// which failed. It does not reimplement pytest, and it does not parse its output beyond
// the exit status — the failure detail belongs to pytest, and repeating it here would be
// a second source of truth for the same run.
//
// # Absent pytest is ADVISORY, absent suites is a FAILURE
//
// The two absences are not the same and must not report the same way. Without `pytest`
// on PATH nothing can be concluded, and blocking a delivery on a tool the operator may
// legitimately not have installed is how a gate gets bypassed. But a skills tree with no
// suites at all means the discovery is broken or the tree moved — and a green verdict
// over a scan that found nothing is precisely the defect this gate exists to catch.
package skillsuites

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrSuitesFailed means at least one skill suite is red.
var ErrSuitesFailed = errors.New("a skill test suite is red — these suites guard the gates that guard everything else, and nothing else runs them")

// ErrNoSuitesFound means discovery found no suite to run.
var ErrNoSuitesFound = errors.New("no skill suite found — discovery is broken or the skills tree moved, and a green verdict over a scan that found nothing is worth less than no verdict")

// Result is one suite's outcome.
type Result struct {
	Skill  string
	Passed bool
	Tail   string // last line of pytest output — the summary, for the operator
}

// Outcome reports the run.
type Outcome struct {
	Advisory bool // pytest absent; nothing was concluded
	Results  []Result
}

// Failed lists the red suites.
func (o Outcome) Failed() []Result {
	var out []Result
	for _, r := range o.Results {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}

// Config selects the tree and bounds the run.
type Config struct {
	RepoRoot  string
	SkillsRel string
	// PerSuiteTimeout bounds one suite. Zero means the default.
	PerSuiteTimeout time.Duration
}

const defaultPerSuiteTimeout = 3 * time.Minute

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if cfg.SkillsRel == "" {
		cfg.SkillsRel = filepath.Join(".claude", "skills")
	}
	if cfg.PerSuiteTimeout == 0 {
		cfg.PerSuiteTimeout = defaultPerSuiteTimeout
	}

	if _, err := exec.LookPath("pytest"); err != nil {
		if _, err := exec.LookPath("python3"); err != nil {
			return Outcome{Advisory: true}, nil
		}
	}

	root := filepath.Join(cfg.RepoRoot, cfg.SkillsRel)
	entries, err := os.ReadDir(root)
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: %s", ErrNoSuitesFound, root)
	}

	var out Outcome
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(root, e.Name())
		if fi, statErr := os.Stat(filepath.Join(skillDir, "tests")); statErr != nil || !fi.IsDir() {
			continue
		}
		out.Results = append(out.Results, runSuite(skillDir, e.Name(), cfg.PerSuiteTimeout))
	}

	if len(out.Results) == 0 {
		return out, fmt.Errorf("%w: %s", ErrNoSuitesFound, root)
	}

	sort.Slice(out.Results, func(i, j int) bool { return out.Results[i].Skill < out.Results[j].Skill })

	if len(out.Failed()) > 0 {
		return out, ErrSuitesFailed
	}
	return out, nil
}

func runSuite(dir, skill string, timeout time.Duration) Result {
	// `python3 -m pytest` and not `pytest`: the suites import their own `scripts/` via a
	// conftest that manipulates sys.path, and the module form keeps the interpreter that
	// resolves them the same one the operator has.
	cmd := exec.Command("python3", "-m", "pytest", "tests/", "-q")
	cmd.Dir = dir

	done := make(chan struct{})
	var output []byte
	var runErr error
	go func() {
		output, runErr = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return Result{Skill: skill, Passed: false, Tail: fmt.Sprintf("timed out after %s", timeout)}
	}

	return Result{Skill: skill, Passed: runErr == nil, Tail: lastMeaningfulLine(string(output))}
}

func lastMeaningfulLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}
