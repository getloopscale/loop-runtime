// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package domainsinkpurity is a ratchet over the persistence types that cross
// the boundary of the services DAG's sink.
//
// # The finding
//
// `api/internal/deploycontracts` documents itself as the pure sink of
// the services acyclic DAG: every context may import it, it imports no sibling.
// Measured 2026-08-12, it exposed four `api/internal/db` symbols in its PUBLIC
// surface — `db.DBTX` (a transaction handle), `db.Build`, `db.Release` and
// `db.BuildAppSpec` — plus three `manifests.*` application types. Two of the four
// are gone as of 2026-09-04 (B-079): `db.Build` and `db.Release` were replaced by
// sink-owned types, and `delivery.go` no longer imports persistence. **38
// production files in 11 packages depend on the sink** — re-measured 2026-09-04;
// the 35-in-10 carried here since 2026-08-12 had drifted — so all of them still
// depend transitively on the two that remain.
//
// `servicesacyclic` cannot see this: it inspects the edges BETWEEN the eight
// services subpackages (nine of them), not the types that cross the DAG's outer
// boundary.
//
// # Why a ratchet, and not a fence
//
// Removing them cost 63 call sites — lines outside _test.go across api/,
// recounted 2026-08-12 and reproduced identically on 2026-08-19 and 2026-09-04
// after review caught the first figure mixing three different counting rules.
// 34 of the 63 are `WithLock`, the environment-lock primitive Unbreakable Rule 6
// puts on every mutation. Refactoring it without the cleanroom that
// `cleanroom-up-live-test-mandate.md` mandates for LIVE validation would trade a
// measured leak for an unmeasured risk, so it stays frozen.
//
// Note what the 2026-09-04 slice did NOT do to that number: taking `db.Build`
// and `db.Release` off the surface left all 63 lines in place — they name a
// contract type now instead of a persistence one. The 63 is the cost of removal,
// not a balance removal draws down.
//
// So the gate freezes what remains measured and refuses a new one. It does not
// claim the leak is closed; it claims the leak stopped growing and is now two
// symbols smaller, which is the property the 38 dependents actually care about.
// The rest of the removal is tracked as its own item with the call sites counted.
//
// # It freezes a SET, not a COUNT
//
// A ratchet on a number lets one symbol be swapped for another and stay green.
// The comparison is set-wise in both directions: a symbol that APPEARS fails, a
// symbol that DISAPPEARS is reported so the frozen set can be tightened rather
// than drifting quietly away from reality.
package domainsinkpurity

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrSinkLeakGrew means a persistence symbol crossed the sink's boundary that
// the frozen set does not carry.
var ErrSinkLeakGrew = errors.New("the domain sink references a persistence symbol beyond the frozen set — the leak grew, and it reaches every package that depends on the sink")

// ErrSinkNotFound means the sink package is absent or holds no parseable Go
// file. The gate examined nothing, so it cannot vouch for anything.
var ErrSinkNotFound = errors.New("the domain sink package was not found or holds no Go file — the gate examined nothing, so it cannot vouch for anything")

// sinkDir is the package under ratchet, relative to the repo root.
const sinkDir = "api/internal/deploycontracts"

// persistenceImportPath is the package under ratchet, matched by PATH.
//
// The local identifier is resolved per file rather than assumed to be "db".
// Assuming it was the whole bypass: `import persistence "…/api/internal/db"`
// made the extractor find NOTHING and the gate report PASS over a leak it could
// not see. A ratchet keyed on a name a caller chooses is a ratchet the caller
// can unlock.
// watchedImportPaths maps an import path to the prefix its findings are keyed
// under. The KEY is what makes the ratchet survive a rename.
//
// `api/internal/db/dbtx` reports under `db`, not under `dbtx`: slice-6 moved the
// SQL access port there and left `db` re-exporting it by TYPE ALIAS, so
// `db.DBTX` and `dbtx.DBTX` are the same type. Keyed by base name, changing one
// import in the sink would report `db.DBTX` as GONE — which this gate's own
// Outcome documents as "progress that should be locked in" — while the boundary
// stayed crossed by an identical type. Measured with a fixture before the fix:
// `Found=[] New=[] Gone=[db.DBTX] err=<nil>`.
//
// A ratchet that reads a rename as progress is worse than no ratchet: it goes
// green precisely when someone routes around it.

var watchedImportPaths = map[string]string{
	"github.com/getloopscale/loop-runtime/api/internal/db":      "db",
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx": "db", // alias of the above — the SAME types
	// db/store is persistence too, and was a blind spot of BOTH gates:
	// dbgeneratedboundary fences only db/generated (and db in server/routes).
	"github.com/getloopscale/loop-runtime/api/internal/db/store": "store",
	// Application-layer packages. ADR D2 of slice-4 promised the manifests import
	// was frozen and the first implementation did not watch them at all — three
	// public references crossed with the gate green.
	"github.com/getloopscale/loop-runtime/api/internal/manifests":   "manifests",
	"github.com/getloopscale/loop-runtime/api/internal/builds/argo": "argo",
	"github.com/getloopscale/loop-runtime/api/internal/gitops":      "gitops",
}

// ErrUnqualifiedImport means a watched package is dot-imported, which makes every
// reference to it unqualified and therefore beyond this gate's static reach.
//
// It is a HARD finding rather than a skip. The previous doc-comment CLAIMED the
// caller reported it; nothing did, and a dot-import passed silently over two new
// public leaks. A comment asserting a guard that does not exist is worse than no
// guard: the next reader stops looking.
var ErrUnqualifiedImport = errors.New("a watched package is dot-imported into the sink — every reference to it becomes unqualified, putting the boundary beyond static reach")

// FrozenDefault is the set measured on 2026-08-12 (slice-4). Each entry is a
// leak that EXISTS and is tracked for removal — never an endorsement.
var FrozenDefault = []string{
	// Each of these stays for a MEASURED reason, and not by omission (B-079, DoD:
	// "the 4 leave the surface, OR each one that stays has a written reason").
	// Count redone on 2026-08-19, production lines in `api/`, a single rule.

	// 34 call sites — the LARGEST cost of the four, and the only one that is also a
	// risk. `WithLock` is the environment-lock primitive that Unbreakable Rule 6 puts
	// on EVERY mutation of (tenant, project, environment). Changing its signature
	// touches the point every write in the product passes through, and
	// `cleanroom-up-live-test-mandate.md` makes LIVE validation mandatory for that.
	// It is not refactoring work: it is release work.
	"db.DBTX", // shared_contracts.go:207 — WithLock(..., fn func(tx db.DBTX) error)

	// 18 call sites. It leaves together with the manifest renderer, not before: the
	// type crosses the boundary because the sink describes the apps the manifest
	// will render, and changing only this side would force converting at the 18
	// sites back to the same format.
	"db.BuildAppSpec", // manifest_builder.go:23 — Apps []db.BuildAppSpec

	// REMOVED on 2026-09-04 (B-079): `db.Release` (6 call sites) and `db.Build`
	// (5) were the cheapest pair and left together, as this list prescribed. Today
	// `delivery.go` exposes `RollbackTarget` and `ActivationBuild` — the sink's own
	// types — and imports no persistence at all. The frozen set shrank from 7 to 5
	// in the same commit in which the surface shrank: a ratchet that stays larger
	// than reality stops being a ratchet and becomes folklore.

	// Application-layer types crossing the same boundary. ADR D2 promised these
	// were frozen and the first implementation did not watch them at all — three
	// public references crossed with the gate green. Measured 2026-08-12 in
	// manifest_builder.go, which also imports api/internal/manifests at :11.
	"manifests.AppManifestInput",  // manifest_builder.go:44,45 — BuildManifestApps return
	"manifests.AutoscalingConfig", // manifest_builder.go — app autoscaling passthrough
	"manifests.CustomDomainInput", // manifest_builder.go:69,79 — custom-domain attachment
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	// Frozen defaults to FrozenDefault. Overridable for tests only.
	Frozen []string
}

// Outcome is the summary. New and Gone are reported separately because they mean
// opposite things: New is a regression, Gone is progress that should be locked in.
type Outcome struct {
	// FrozenCount is the size of the set this run compared against — the
	// EFFECTIVE set, not the package global, so an overridden Config does not
	// print a number from somewhere else.
	FrozenCount int
	Found       []string
	New         []string
	Gone        []string
	// FilesParsed lets the caller tell "nothing leaked" from "nothing was read".
	FilesParsed int
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

// Check parses the sink and compares its persistence surface to the frozen set.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	frozen := cfg.Frozen
	if len(frozen) == 0 {
		frozen = FrozenDefault
	}

	dir := filepath.Join(cfg.RepoRoot, sinkDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrSinkNotFound, sinkDir)
	}

	found := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		// Test files are excluded: a fixture naming a persistence type is not
		// the package's public surface, and counting it would make the ratchet
		// fire on test edits that change nothing for the 35 dependents.
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return out, fmt.Errorf("parsing %s/%s: %w", sinkDir, name, err)
		}
		out.FilesParsed++
		if err := collectRefs(f, found); err != nil {
			return out, fmt.Errorf("%s/%s: %w", sinkDir, name, err)
		}
	}

	if out.FilesParsed == 0 {
		return out, fmt.Errorf("%w: %s", ErrSinkNotFound, sinkDir)
	}

	out.FrozenCount = len(frozen)
	out.Found = sortedKeys(found)
	frozenSet := map[string]bool{}
	for _, s := range frozen {
		frozenSet[s] = true
	}
	for s := range found {
		if !frozenSet[s] {
			out.New = append(out.New, s)
		}
	}
	for s := range frozenSet {
		if !found[s] {
			out.Gone = append(out.Gone, s)
		}
	}
	sort.Strings(out.New)
	sort.Strings(out.Gone)

	if len(out.New) > 0 {
		out.Violation = ErrSinkLeakGrew
		return out, nil
	}
	return out, nil
}

// collectRefs records every reference to a watched package, under whatever
// local names THIS file imports it by.
//
// Three properties this had to learn the hard way, each measured by probing:
//
//  1. The local name is resolved per file, never assumed to be the base name.
//     With it hardcoded, `import persistence ".../api/internal/db"` produced ZERO
//     findings and a PASS. A ratchet keyed on a name the caller chooses is one
//     the caller unlocks.
//  2. ALL local names are collected, not the first. Go permits importing one
//     path twice under different names, and returning on the first match let
//     `dbx.AppSecret` cross with the gate green.
//  3. An identifier that resolves to a LOCAL declaration is not the package.
//     `db := logger{}` inside an unexported function was reported as
//     "NEW LEAK db.Close" — a finding about something that crosses no
//     boundary at all. A ratchet that fires on non-leaks is one somebody
//     switches off, which is the same argument this file makes for AST over grep.
func collectRefs(f *ast.File, into map[string]bool) error {
	// local identifier -> the canonical prefix findings are keyed under
	names := map[string]string{}
	for path, prefix := range watchedImportPaths {
		locals, dotImported := localNamesFor(f, path)
		if dotImported {
			return fmt.Errorf("%w: %s", ErrUnqualifiedImport, path)
		}
		for _, l := range locals {
			names[l] = prefix
		}
	}
	if len(names) == 0 {
		return nil
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// Obj is non-nil exactly when the identifier resolved to a declaration in
		// this file — a variable, parameter or type shadowing the import name. A
		// package qualifier never resolves that way.
		if ident.Obj != nil {
			return true
		}
		prefix, watched := names[ident.Name]
		if !watched {
			return true
		}
		into[prefix+"."+sel.Sel.Name] = true
		return true
	})
	return nil
}

// localNamesFor returns every identifier this file uses for an imported path,
// plus whether it is dot-imported. Empty when the file does not import it.
func localNamesFor(f *ast.File, path string) (names []string, dotImported bool) {
	for _, imp := range f.Imports {
		if imp.Path == nil || strings.Trim(imp.Path.Value, `"`) != path {
			continue
		}
		if imp.Name == nil {
			names = append(names, filepath.Base(path))
			continue
		}
		switch imp.Name.Name {
		case ".":
			return nil, true
		case "_":
			// A blank import brings no symbol into scope.
		default:
			names = append(names, imp.Name.Name)
		}
	}
	return names, false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Summary renders the one-line verdict. The frozen count is printed on every
// run, green or red: a debt that is only visible when it grows is a debt nobody
// remembers to pay down.
func (o Outcome) Summary() string {
	return fmt.Sprintf("%d file(s) parsed, %d watched symbol(s) crossing the sink boundary — persistence and application layer (%d frozen, tracked for removal), %d new, %d gone",
		o.FilesParsed, len(o.Found), o.FrozenCount, len(o.New), len(o.Gone))
}

// ActionableFix is the operator-facing message for a FAIL.
func ActionableFix() string {
	return "A package outside the sink's declared responsibility now crosses its public\n" +
		"boundary, and every package that imports the sink inherits the dependency.\n" +
		"Either keep the symbol out of an exported signature, or — if the crossing is\n" +
		"deliberate — add it to FrozenDefault with a file:line comment saying why, and\n" +
		"record it on B-079. Growing the frozen set is a decision; growing it silently\n" +
		"is the defect this gate exists to prevent."
}
