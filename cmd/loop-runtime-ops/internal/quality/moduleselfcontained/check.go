// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
//
// # A dependency it cannot fetch is not a verdict about the module
//
// `api` and `pkg` require `github.com/getloopscale/contracts`, a PRIVATE module. A job
// that runs without a credential for it — the fork-safe CI job, by design — cannot
// download it, and `go build` stops before the compiler. That says nothing about whether
// the module stands alone, so such a module is reported NOT VERIFIED and named, apart from
// both a pass and a failure. Only an authentication refusal counts: a version that does
// not exist upstream (a tag never pushed) breaks every isolated build and stays a FAIL.
package moduleselfcontained

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/lintcoverage"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
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

// UnreachableDependencyError means a module's build stopped because a dependency could
// not be fetched for lack of a credential. It is a Builder's way to say the build proved
// nothing, rather than that the module failed.
type UnreachableDependencyError struct {
	Dependencies []string
	Output       string
}

func (e *UnreachableDependencyError) Error() string {
	return fmt.Sprintf("dependency not fetchable without a credential: %s", strings.Join(e.Dependencies, ", "))
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
	// Unreachable lists the modules whose build could not start because a dependency
	// refused this machine's (absent) credential. They were not verified.
	Unreachable []Failure
	Reason      string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation, NotVerified when a module could
// not be built for lack of a credential to fetch a dependency, and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Violation != nil:
		return verdict.Fail
	case len(o.Unreachable) > 0:
		return verdict.NotVerified
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
		berr := builder.BuildWithoutWorkspace(root, m)
		var unreachable *UnreachableDependencyError
		switch {
		case berr == nil:
		case errors.As(berr, &unreachable):
			out.Unreachable = append(out.Unreachable, Failure{Module: m, Err: berr.Error()})
		default:
			out.Failed = append(out.Failed, Failure{Module: m, Err: berr.Error()})
		}
	}
	sort.Slice(out.Failed, func(i, j int) bool { return out.Failed[i].Module < out.Failed[j].Module })
	sort.Slice(out.Unreachable, func(i, j int) bool { return out.Unreachable[i].Module < out.Unreachable[j].Module })

	if len(out.Failed) > 0 {
		f := out.Failed[0]
		out.Violation = fmt.Errorf("%w: %s — %s", ErrNotSelfContained, f.Module, firstLine(f.Err))
		return out, nil
	}
	verified := out.Checked - len(out.Unreachable)
	out.Reason = fmt.Sprintf("%d module(s) build without the workspace (GOWORK=off), which is what CI does", verified)
	if len(out.Unreachable) > 0 {
		names := make([]string, 0, len(out.Unreachable))
		for _, u := range out.Unreachable {
			names = append(names, u.Module)
		}
		out.Reason += fmt.Sprintf("; %d NOT VERIFIED (%s) — a dependency refused this machine's credential, so the build never reached the compiler",
			len(out.Unreachable), strings.Join(names, ", "))
	}
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
		text := strings.TrimSpace(string(out))
		if deps, ok := unreachableDependencies(text); ok {
			return &UnreachableDependencyError{Dependencies: deps, Output: text}
		}
		return fmt.Errorf("%s", text)
	}
	// `go build` exits 0 even when it prints "updates to go.mod needed" — measured on
	// 2026-08-18, and that is why the inconsistency survived five days. The text is the
	// signal, not the exit code.
	if strings.Contains(string(out), "updates to go.mod needed") {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// reFetchFailure is the line `go build` prints when it cannot read a dependency's go.mod.
var reFetchFailure = regexp.MustCompile(`reading (\S+)/go\.mod at revision \S+:`)

// reCompilerDiagnostic is a `file.go:line:col:` diagnostic.
var reCompilerDiagnostic = regexp.MustCompile(`(?m)^\s*\S+\.go:\d+:\d+: .*$`)

// credentialRefusals are what git prints when the remote wants a credential this
// machine does not have. An unknown revision is deliberately absent: it is a defect.
var credentialRefusals = []string{
	"terminal prompts disabled",
	"could not read Username",
	"Permission denied (publickey)",
	"Authentication failed",
}

// unreachableDependencies reports the dependencies a failed build could not fetch for
// lack of a credential — and only when that is the WHOLE failure. Any compiler
// diagnostic that is not such a fetch, or a go.mod inconsistency, keeps the build a
// defect: an access failure must never hide one.
func unreachableDependencies(output string) ([]string, bool) {
	refused := false
	for _, marker := range credentialRefusals {
		if strings.Contains(output, marker) {
			refused = true
			break
		}
	}
	if !refused || strings.Contains(output, "updates to go.mod needed") {
		return nil, false
	}
	for _, diag := range reCompilerDiagnostic.FindAllString(output, -1) {
		if !reFetchFailure.MatchString(diag) {
			return nil, false
		}
	}
	seen := map[string]bool{}
	var deps []string
	for _, m := range reFetchFailure.FindAllStringSubmatch(output, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			deps = append(deps, m[1])
		}
	}
	if len(deps) == 0 {
		return nil, false
	}
	sort.Strings(deps)
	return deps, true
}
