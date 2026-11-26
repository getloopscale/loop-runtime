// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrSuitesFailed means at least one skill suite is red.
var ErrSuitesFailed = errors.New("a skill test suite is red — these suites guard the gates that guard everything else, and nothing else runs them")

// ErrSkillsRootMissing means the skills tree is absent. It is the installed kit,
// which .gitignore excludes, so this is the state of every fresh clone: nothing
// was verified, and that is named apart from a tree that holds no suite.
var ErrSkillsRootMissing = errors.New("the skills tree is absent — it is the installed, gitignored kit, so nothing was verified on this machine")

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
	// Violation is what the run found wrong (ErrSuitesFailed, ErrNoSuitesFound),
	// or nil. A violation is the check's verdict, never its error.
	Violation error
	// Unverified is why nothing could be verified on this machine
	// (ErrSkillsRootMissing: the gitignored kit is not installed), or nil.
	Unverified error
}

// Verdict is Fail on a red suite or on a discovery that found none, Skip when
// the kit is not installed or pytest is absent, NotVerified for any other
// reason nothing could be verified, and Pass otherwise.
//
// A missing kit is Skip, not NotVerified (owner decision, 2026-09-26): the kit
// is a gitignored dependency, so its absence is the normal state of every fresh
// clone, and a gate that fails there fails for something the repository does
// not contain. The skip still names the reason, so it never reads as green.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Violation != nil:
		return verdict.Fail
	case errors.Is(o.Unverified, ErrSkillsRootMissing):
		return verdict.Skip
	case o.Unverified != nil:
		return verdict.NotVerified
	case o.Advisory:
		return verdict.Skip
	}
	return verdict.Pass
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

// SuiteDir is one discovered suite.
type SuiteDir struct {
	Skill string
	Dir   string
}

// Discover finds the suites without running them.
//
// Split from Check on purpose. Running the suites is what the GATE does in
// `task quality:gates`; a unit test that also ran all fifteen would duplicate ~40s of
// work into every `go test ./...` and make this package's result depend on the health of
// fifteen unrelated suites. What a unit test can uniquely assert about the real tree is
// that DISCOVERY reaches it — and that is cheap, deterministic, and exactly the half that
// silently under-reaches when a tree moves.
func Discover(cfg Config) ([]SuiteDir, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if cfg.SkillsRel == "" {
		cfg.SkillsRel = filepath.Join(".claude", "skills")
	}
	root := filepath.Join(cfg.RepoRoot, cfg.SkillsRel)

	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrSkillsRootMissing, root)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}

	var found []SuiteDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		fi, statErr := os.Stat(filepath.Join(dir, "tests"))
		if errors.Is(statErr, fs.ErrNotExist) {
			continue // a skill without tests/ carries no suite
		}
		if statErr != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Join(dir, "tests"), statErr)
		}
		if !fi.IsDir() {
			continue
		}
		found = append(found, SuiteDir{Skill: e.Name(), Dir: dir})
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoSuitesFound, root)
	}
	return found, nil
}

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

	// Discovery first: it needs no interpreter, and a tree holding no suite is a
	// failure whether or not pytest is installed (see the package doc).
	suites, err := Discover(cfg)
	switch {
	case errors.Is(err, ErrSkillsRootMissing):
		return Outcome{Unverified: err}, nil
	case errors.Is(err, ErrNoSuitesFound):
		return Outcome{Violation: err}, nil
	case err != nil:
		return Outcome{}, err
	}

	if !pytestRunnable() {
		return Outcome{Advisory: true}, nil
	}

	var out Outcome
	for _, sd := range suites {
		out.Results = append(out.Results, runSuite(sd.Dir, sd.Skill, cfg.PerSuiteTimeout))
	}

	sort.Slice(out.Results, func(i, j int) bool { return out.Results[i].Skill < out.Results[j].Skill })

	if len(out.Failed()) > 0 {
		out.Violation = ErrSuitesFailed
	}
	return out, nil
}

// pytestRunnable reports whether the exact invocation runSuite makes can start.
//
// It asks the interpreter, not PATH. The earlier probe looked for a `pytest` binary
// and, failing that, settled for `python3` existing — so on a host with python3 and no
// pytest module (a stock CI runner is one) every suite ran, died on "No module named
// pytest", and the gate reported fifteen red suites that nobody had executed. A
// verdict about the suites must not be a verdict about the host's packages.
func pytestRunnable() bool {
	if _, err := exec.LookPath("python3"); err != nil {
		return false
	}
	probe := exec.Command("python3", "-c", "import pytest")
	probe.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	return probe.Run() == nil
}

func runSuite(dir, skill string, timeout time.Duration) Result {
	// `python3 -m pytest` and not `pytest`: the suites import their own `scripts/` via a
	// conftest that manipulates sys.path, and the module form keeps the interpreter that
	// resolves them the same one the operator has.
	//
	// `-p no:cacheprovider` + PYTHONDONTWRITEBYTECODE remove this gate's only shared
	// mutable state. It runs pytest INSIDE the repository's own directories, so a second
	// pytest — another operator, a parallel `go test ./...`, a developer in another
	// terminal — would write the same `.pytest_cache/` and `__pycache__/`. Two processes
	// writing one cache produce a transient import failure in whichever loses the race.
	//
	// Observed once on 2026-08-16: this gate reported `plan-confidence` red inside
	// `task quality:all` while the same suite ran green immediately before and after, and
	// four attempts failed to reproduce it. The collision is a MECHANISM that fits, not a
	// confirmed cause — but a gate whose verdict can depend on who else is running is
	// worthless either way (`rules/testing.md` § 3), and removing the shared state costs
	// two flags.
	cmd := exec.Command("python3", "-m", "pytest", "tests/", "-q", "-p", "no:cacheprovider")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")

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
