// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package ecosystemvalidators runs the two validators the ecosystem's own change
// protocol requires — and which nothing ran.
//
// # The finding
//
// `rules/cycle-rule-schema.md § Golden Rule Change Protocol` requires, for every change
// to a LOCKED golden rule, that *"`check_xrefs.py` and `test_e2e_smoke.py` both PASS"*.
//
// Measured 2026-08-17 while following that protocol for `B-127`:
// `grep -rn "check_xrefs" taskfiles/ cmd/loop-runtime-ops/ .claude/hooks/` **returns
// nothing** — no task, no gate, no hook invokes them. And when they were run for the
// first time, `check_xrefs.py` came out **FAIL with 4 findings**, all introduced the
// same day without anything flagging them.
//
// It is the same shape as `B-124` (three red suites that no task ran), in another
// pair of validators: **a process requirement with no execution is a promise**.
//
// # FAIL and WARN are reported separately, on purpose
//
// `check_xrefs.py` exits 0 with WARNs. A gate that summed the two would let the warning
// erase the failure — and a validator that always reports warnings teaches people to
// ignore it, which is how the 4 FAIL findings would go unnoticed among 6 chronic
// warnings.
package ecosystemvalidators

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrValidatorFailed means at least one validator exited non-zero.
var ErrValidatorFailed = errors.New("an ecosystem validator failed — they are a requirement of the golden-rule change protocol, and until now no gate ran them")

// ErrValidatorsMissing means the validator scripts are not on disk.
//
// Absence is a FAILURE, never a skip: the gate exists because nobody ran them, and
// reporting green for not finding them would reproduce the defect with the look of
// coverage.
var ErrValidatorsMissing = errors.New("the ecosystem validators are not on disk — a gate that cannot find what it audits cannot report green")

// Validator is one script to run.
type Validator struct {
	Name string
	Rel  string
	// Args are the arguments passed to the script, and they exist because one of
	// them has TWO legitimate questions and the caller decides which one it is asking.
	//
	// `verify_ecosystem.py` by default grades the last 40 commits, which is the
	// right question for a standalone audit: does the repository follow its
	// conventions? This package runs in the chain
	// `.git/hooks/pre-push` -> `task quality:gates` -> here, and the question there
	// is a different one: may this push land? There, only the violations the pusher
	// can still fix count.
	//
	// Measured 2026-09-16: with `-40`, three of the four findings were already on
	// `origin/workspace` and the two nearest would leave the window in 11 and 13
	// commits — which could not happen, because the gate refused the commits that
	// would move the window. Each piece was right and together they were a deadlock.
	Args []string
}

// DefaultValidators are the two the change protocol names.
//
// Paths follow the kit's `scripts/ → mechanisms/<family>/` relocation
// (the same one recorded in `mechanisms/distribution/install.sh` and the
// `check_install_drift.py` docstring). `test_e2e_smoke.py` was renamed to
// `verify_ecosystem.py` in the same pass — the smoke chain is now one section
// inside a broader ecosystem verification. The Rels here follow the kit's
// current on-disk layout; anchoring them elsewhere makes the gate hunt for
// files that were moved deliberately.
var DefaultValidators = []Validator{
	{Name: "check_xrefs", Rel: "mechanisms/gates/check_xrefs.py"},
	{Name: "verify_ecosystem", Rel: "mechanisms/gates/verify_ecosystem.py", Args: []string{"--introduced"}},
}

// Result is one validator's outcome.
type Result struct {
	Name   string
	Passed bool
	Warns  int
	Tail   string
}

// Outcome reports the run.
type Outcome struct {
	// Advisory means nothing was concluded: the interpreter or the kit itself
	// is absent. AdvisoryReason says which.
	Advisory       bool
	AdvisoryReason string
	Results        []Result
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when a validator failed, Skip when nothing was concluded
// (the kit or python3 is absent; the gate is advisory then), and Pass
// otherwise. Warnings never fail it.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Violation != nil:
		return verdict.Fail
	case o.Advisory:
		return verdict.Skip
	}
	return verdict.Pass
}

// Failed lists the validators that exited non-zero.
func (o Outcome) Failed() []Result {
	var out []Result
	for _, r := range o.Results {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}

// Warnings totals the advisory findings across validators.
func (o Outcome) Warnings() int {
	n := 0
	for _, r := range o.Results {
		n += r.Warns
	}
	return n
}

// Config selects the tree.
type Config struct {
	RepoRoot   string
	EcoRel     string // ecosystem dir; empty resolves the layout
	Validators []Validator
	Timeout    time.Duration
}

const defaultTimeout = 2 * time.Minute

// kitInstalled reports whether eco holds the kit's mechanisms tree. The kit is
// an installed, gitignored dependency in consumer repositories (this one
// included), so a clone may legitimately not have it.
func kitInstalled(eco string) bool {
	fi, err := os.Stat(filepath.Join(eco, "mechanisms"))
	return err == nil && fi.IsDir()
}

// resolveEco returns the ecosystem directory, honouring both layouts.
//
// Plugin (every consumer) has the ecosystem under `.claude/`; standalone (the kit's
// own repo) has it at the root. Pinning one of the two would make the gate report
// absence in one of the layouts — which is the `B-120` defect, elsewhere.
//
// The probe moved from `scripts/` to `mechanisms/` because the kit relocated
// everything into `mechanisms/<family>/`. `scripts/` no longer exists; probing for it
// is probing for a directory no layout carries, and the gate falls back to `root` in
// both.
func resolveEco(root, rel string) string {
	if rel != "" {
		return filepath.Join(root, rel)
	}
	if fi, err := os.Stat(filepath.Join(root, ".claude", "mechanisms")); err == nil && fi.IsDir() {
		return filepath.Join(root, ".claude")
	}
	return root
}

// Check runs every validator.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	vals := cfg.Validators
	if vals == nil {
		vals = DefaultValidators
	}
	eco := resolveEco(cfg.RepoRoot, cfg.EcoRel)

	if !kitInstalled(eco) {
		return Outcome{Advisory: true, AdvisoryReason: "the ecosystem kit is not installed here (no mechanisms/ under " +
			eco + "); it is a gitignored dependency, so a clone without it has nothing to run"}, nil
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return Outcome{Advisory: true, AdvisoryReason: "python3 is not on PATH"}, nil
	}

	var out Outcome
	for _, v := range vals {
		script := filepath.Join(eco, v.Rel)
		if _, err := os.Stat(script); err != nil {
			return out, fmt.Errorf("%w: %s", ErrValidatorsMissing, script)
		}
		out.Results = append(out.Results, run(cfg.RepoRoot, script, v.Name, v.Args, cfg.Timeout))
	}
	if len(out.Failed()) > 0 {
		out.Violation = ErrValidatorFailed
		return out, nil
	}
	return out, nil
}

func run(root, script, name string, args []string, timeout time.Duration) Result {
	// G204: the path does not come from user input. It is `DefaultValidators`
	// (package constants) joined to the resolved root, and `Check` has already proven
	// the file exists before getting here. The configurable `Validators` exists so
	// tests can build a tree of their own.
	cmd := exec.Command("python3", append([]string{script}, args...)...) //nolint:gosec // path and args come from package constants, not from input
	cmd.Dir = root

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
		return Result{Name: name, Passed: false, Tail: fmt.Sprintf("timed out after %s", timeout)}
	}

	body := string(output)
	return Result{
		Name:   name,
		Passed: runErr == nil,
		Warns:  strings.Count(body, "[WARN]"),
		Tail:   lastMeaningfulLine(body),
	}
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
