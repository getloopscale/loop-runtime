// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package moduleselfcontained refuses a Go module that only builds inside the workspace.
//
// # The defect, measured
//
// On 2026-08-18, while trying to carry a fix to the droplet, I found that CI **had not
// published any image since 2026-08-09**. Cause:
//
//	operators/api/go.mod   k8s.io/apimachinery v0.36.0   <- bumped on Aug 13, a CVE fix
//	operators/go.mod       k8s.io/api          v0.35.3   <- left behind
//
// `operators` **depends on** `operators/api`. Under MVS the higher version wins, so the module
// must declare ≥0.36.0. It did not, and the build died with
// `go: updates to go.mod needed`.
//
// **And nothing failed for five days**, because `go.work` unions the modules and resolves across
// them — a local `go build ./...` passes. The Dockerfile uses `GOWORK=off` (like any isolated
// build), and that is where the inconsistency shows up. `CLAUDE.md` already warns that *"`go build
// ./...` from the root does NOT cover the repo"*; what was missing was somebody checking.
//
// # The invariant
//
// Every module builds **without** the workspace. It is the condition CI requires and that
// local development never exercises — exactly the shape of defect that ages in silence.
//
// # The cost, stated out loud
//
// This gate really builds each module, with a cold module cache the first time. It is
// expensive, and it is expensive for the same reason it is useful: it is what CI does. It runs
// in `quality:gates`, not in every test.
package moduleselfcontained

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lintcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// ErrNotSelfContained means a module does not build without the workspace.
var ErrNotSelfContained = errors.New("module does not build without the workspace — CI will not be able to build it")

// Builder compiles one module in isolation. An injectable seam (DIP): the test must
// FABRICATE the failure, and a real build would take minutes and depend on the tree.
type Builder interface {
	BuildWithoutWorkspace(root, module string) error
}

// Config drives the check.
type Config struct {
	RepoRoot string
	Builder  Builder
}

// Failure is one module that does not stand alone.
type Failure struct {
	Module string
	Err    string
}

// Outcome reports what was established.
type Outcome struct {
	Checked int
	Failed  []Failure
	Reason  string
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

// Check builds every module with the workspace disabled.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	builder := cfg.Builder
	if builder == nil {
		builder = execBuilder{}
	}

	// Reuses `lintcoverage`'s discovery instead of reimplementing it: the lesson there is
	// that a hand-kept list ages toward the invisible side, and two lists diverge.
	mods, err := lintcoverage.DiscoverModules(root)
	if err != nil {
		return out, err
	}

	// ALL are built, even when one fails — aborting at the first hides the ones after it,
	// which is the defect B-121 measured in `task lint`.
	for _, m := range mods {
		out.Checked++
		if berr := builder.BuildWithoutWorkspace(root, m); berr != nil {
			out.Failed = append(out.Failed, Failure{Module: m, Err: berr.Error()})
		}
	}
	sort.Slice(out.Failed, func(i, j int) bool { return out.Failed[i].Module < out.Failed[j].Module })

	if len(out.Failed) > 0 {
		f := out.Failed[0]
		out.Violation = fmt.Errorf("%w: %s — %s", ErrNotSelfContained, f.Module, firstLine(f.Err))
		return out, nil
	}
	out.Reason = fmt.Sprintf("%d module(s) build without the workspace (GOWORK=off), which is what CI does", out.Checked)
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// execBuilder runs the real compiler with the workspace disabled.
type execBuilder struct{}

func (execBuilder) BuildWithoutWorkspace(root, mod string) error {
	// #nosec G204 -- literal arguments; `dir` derives from the go.mod scan under the root.
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = lintcoverage.ModuleDir(root, mod)
	cmd.Env = append(cmd.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	// `go build` exits 0 even when it prints "updates to go.mod needed" — measured on
	// 2026-08-18, and that is why the inconsistency survived five days. The text is the
	// signal, not the exit code.
	if strings.Contains(string(out), "updates to go.mod needed") {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}
