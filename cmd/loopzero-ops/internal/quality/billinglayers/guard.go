// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrGuardUnresolved means a declared guard does not actually guard anything.
var ErrGuardUnresolved = errors.New("declared guard does not resolve")

// ErrSiblingAbsent means a layer lives in a repository that is not checked out.
//
// It is NOT a failure: a partial checkout must not block every push. It is reported as
// PARTIAL coverage, under the contract of `rules/reference-provenance.md § 4` — what must
// not happen is a silent green over what was not looked at.
var ErrSiblingAbsent = errors.New("sibling repository absent")

// openStatuses are the states in which a debt still has an owner. `planned`
// and `approved` count as open: the ratchet exists to catch CLOSING without
// coverage, and a planned or approved item is not closed.
var openStatuses = map[string]bool{
	"raw": true, "triaged": true, "approved": true, "planned": true,
}

// terminalStatuses close a debt: an `open:` guard pointing at one fails.
var terminalStatuses = map[string]bool{"shipped": true, "killed": true}

// GuardResolver answers whether a declared guard actually guards.
//
// It receives the roots as fields, and neither reads `os.Getenv` nor assumes the current
// directory: a collaborator that reads the process cannot be substituted in a test.
type GuardResolver struct {
	repoRoot    string
	siblingRoot string
	debts       map[string]Debt
}

// NewGuardResolver builds the resolver used by Check and by the tests alike.
// debts is the registry's versioned [debts] ledger.
func NewGuardResolver(repoRoot, siblingRoot string, debts map[string]Debt) *GuardResolver {
	return &GuardResolver{repoRoot: repoRoot, siblingRoot: siblingRoot, debts: debts}
}

// Root returns the filesystem root for a declared repo name.
func (g *GuardResolver) Root(repo string) string {
	if repo == "loopzero" {
		return g.repoRoot
	}
	return g.siblingRoot
}

// Resolve reports whether the guard holds.
func (g *GuardResolver) Resolve(guard Guard, repo string) error {
	switch guard.Kind {
	case GuardGate:
		return g.resolveGate(guard.Target)
	case GuardTest:
		return g.resolveFile(guard.Target, repo, "test")
	case GuardOpen:
		return g.resolveDebt(guard.Target)
	default:
		return fmt.Errorf("%w: form %q", ErrGuardUnresolved, guard.Kind)
	}
}

// resolveGate requires the gate to be RUN, not merely to exist.
//
// The check is against `taskfiles/quality.yml` — the place where a gate starts being
// executed — and not against the package existing. A gate implemented and not switched on
// is the "implemented but not wired" pattern from `CLAUDE.md`, and guards exactly nothing.
func (g *GuardResolver) resolveGate(name string) error {
	path := filepath.Join(g.repoRoot, "taskfiles", "quality.yml")
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("%w: could not read %s: %w", ErrGuardUnresolved, path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	target := "quality " + name
	for sc.Scan() {
		if strings.Contains(sc.Text(), target) {
			return nil
		}
	}
	return fmt.Errorf("%w: the gate %q is not invoked in taskfiles/quality.yml — a gate nobody runs guards nothing",
		ErrGuardUnresolved, name)
}

func (g *GuardResolver) resolveFile(rel, repo, what string) error {
	root := g.Root(repo)
	if root == "" {
		return fmt.Errorf("%w: %s in %s", ErrSiblingAbsent, rel, repo)
	}
	if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
		return fmt.Errorf("%w: %s %q does not exist in %s", ErrGuardUnresolved, what, rel, repo)
	}
	return nil
}

// resolveDebt is the RATCHET.
//
// A debt guards only while it has an owner. The moment the registry records
// the item as shipped or killed, the layer is uncovered and the gate fails,
// forcing the guard to become `gate:` or `test:`. Without this clause `open:`
// would be a rug: closing the item would silence the layer while the registry
// still claimed somebody owned it.
func (g *GuardResolver) resolveDebt(item string) error {
	d, ok := g.debts[item]
	if !ok {
		return fmt.Errorf("%w: the debt %s is not declared in the [debts] section of %s — a debt without an item owns nothing",
			ErrGuardUnresolved, item, RegistryRelPath)
	}
	if !openStatuses[d.Status] {
		return fmt.Errorf("%w: the debt %s is %q — it left the open states and the layer is uncovered; swap the guard for `gate:` or `test:` in the registry",
			ErrGuardUnresolved, item, d.Status)
	}
	return nil
}
