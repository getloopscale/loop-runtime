// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package qualitycli is the command-line layer of `loopzero-ops quality`: the gate
// registry, the one runner every gate goes through, and each gate's adapter
// (its flags and how it reports its check's outcome).
//
// The checks themselves live in internal/quality/<gate>. This package decides
// nothing about a repository; it turns a check's outcome into output and an
// exit status, the same way for every gate.
//
// ONE FILE PER GATE. A gate registers itself from its own file, next to its
// adapter: adding a gate is adding one file here (plus its wiring in
// taskfiles/quality.yml, or a Wiring exemption on the spec). There is no
// central list to edit, and the file name is the gate name, so where a gate
// lives is never a matter of which theme someone filed it under.
package qualitycli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/exitcode"
)

// Outcome is a gate's verdict once its check has run.
type Outcome int

const (
	// Pass — the check ran and found nothing wrong.
	Pass Outcome = iota
	// Fail — the check ran and found a violation.
	Fail
)

// Invocation is what a gate's check receives: the resolved repository root
// and where to write.
type Invocation struct {
	Name   string
	Root   string // resolved repository root; empty when the gate takes no --root
	Stdout io.Writer
	Stderr io.Writer
}

// Check runs a gate once its flags are parsed.
//
// It returns Pass or Fail when it reached a verdict, and an error when it
// could not — an unreadable input, a tool that errored. The runner reports
// the error and exits Incomplete: a check that could not measure never reads
// as a violation, and never as a pass.
type Check func(Invocation) (Outcome, error)

// errUsage marks an invalid invocation discovered after parsing (a flag value
// the gate cannot use). The runner exits Usage.
var errUsage = errors.New("invalid invocation")

// usageError builds an errUsage with a message for the operator.
func usageError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, args...))
}

// Gate is one `loopzero-ops quality <name>` gate.
type Gate struct {
	Name    string
	Summary string
	// Wiring is why the gate is NOT in taskfiles/quality.yml. The zero value
	// means it must be wired there; TestEveryGateIsWiredOrExempted holds the
	// two in agreement.
	Wiring Wiring
	// Setup registers the gate's own flags (beyond --root) and returns its
	// check.
	Setup func(fs *flag.FlagSet) Check
	// RootDefault overrides the --root default of "."; used by a gate that
	// scans a sub-tree by default.
	RootDefault string
	// NoRoot marks a gate that reads explicit inputs rather than the
	// repository tree, so it takes no --root.
	NoRoot bool
}

var (
	registryMu sync.Mutex
	registry   = map[string]Gate{}
)

// register adds a gate. It panics on a duplicate name or a gate with no
// check: both are wiring bugs, and a duplicate would otherwise collapse
// silently while --list stayed the same.
func register(g Gate) {
	registryMu.Lock()
	defer registryMu.Unlock()
	switch {
	case g.Name == "":
		panic("qualitycli: gate with an empty name")
	case g.Summary == "":
		panic(fmt.Sprintf("qualitycli: gate %q has no summary", g.Name))
	case g.Setup == nil:
		panic(fmt.Sprintf("qualitycli: gate %q has no Setup (registered but not wired)", g.Name))
	}
	if _, dup := registry[g.Name]; dup {
		panic(fmt.Sprintf("qualitycli: duplicate gate name %q", g.Name))
	}
	registry[g.Name] = g
}

// Gates returns every registered gate, sorted by name.
func Gates() []Gate {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]Gate, 0, len(registry))
	for _, g := range registry {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup returns the gate registered under name.
func Lookup(name string) (Gate, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	g, ok := registry[name]
	return g, ok
}

// Run is the one runner every gate goes through: it parses the flags,
// resolves --root, runs the check and maps the result onto loopzero-ops' exit
// table. Written once, so a fix to how gates report reaches all of them.
func (g Gate) Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(g.Name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var root *string
	if !g.NoRoot {
		def := g.RootDefault
		if def == "" {
			def = "."
		}
		root = fs.String("root", def, "repository root directory")
	}
	check := g.Setup(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitcode.OK
		}
		return exitcode.Usage
	}
	inv := Invocation{Name: g.Name, Stdout: stdout, Stderr: stderr}
	if root != nil {
		inv.Root = resolveRoot(*root)
	}
	out, err := check(inv)
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "%s: %v\n", g.Name, err)
		return exitcode.Usage
	case errors.Is(err, errNotVerified):
		return exitcode.Incomplete
	case err != nil:
		fmt.Fprintf(stderr, "%s: ERROR — could not complete the check: %v\n", g.Name, err)
		return exitcode.Incomplete
	case out == Fail:
		return exitcode.Failure
	}
	return exitcode.OK
}

// resolveRoot expands "." to the absolute working directory so messages show
// a concrete path; any other value is used as given. If the working directory
// cannot be read, "." still names it for every relative operation, so it is
// kept rather than replaced by a guess.
func resolveRoot(root string) string {
	if root != "." {
		return root
	}
	abs, err := os.Getwd()
	if err != nil {
		return root
	}
	return abs
}

// firstNonEmpty returns the first non-empty string (flag value > env fallback).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// isViolation reports whether err is one of the sentinels a check returns when
// it measured and found something wrong. Any other error means the check could
// not measure, which the runner reports as incomplete rather than as a verdict.
func isViolation(err error, verdicts ...error) bool {
	for _, v := range verdicts {
		if errors.Is(err, v) {
			return true
		}
	}
	return false
}
