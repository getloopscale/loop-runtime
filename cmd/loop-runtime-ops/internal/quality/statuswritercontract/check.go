// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package statuswritercontract refuses a bare `Status().Update` anywhere in the
// operators tree.
//
// # Why it exists
//
// ADR `2026-08-12-status-writer-contract`: a CRD's `.status` has exactly one
// writer, and it persists with a merge patch taken off a base snapshot. `Update`
// replays the WHOLE object as read at the initial Get, so a status write races any
// concurrent spec or metadata write by another controller — and the loser's change
// vanishes with no error anywhere. A merge patch carries no resourceVersion and
// touches only the fields that changed, so it cannot lose that race.
//
// `B-085` converted 28 such writes across six controllers. Its fourth DoD bullet
// asked for this gate in the same breath, and for a while it did not exist
// (translated from the original Portuguese):
//
//	a static gate refuses any new `Status().Update` in the operators —
//	without it the migration is reversible by carelessness
//
// # Why a gate and not more per-package ratchets
//
// The conversion left behind per-package ratchets — a test file in each package
// reading its own directory. They are stronger than this gate where they exist:
// they also assert the readiness summary, the anchor of the base, and pairing
// against a frozen exemption list.
//
// But a per-package ratchet protects the package somebody remembered to protect.
// It has no way to fail when a NEW package is born without one, and that is
// exactly what happened: an adversarial audit injected a fresh `Status().Update`
// into `operators/internal/environment` — a package with no ratchet — and the
// entire operators suite returned exit 0, fourteen packages `ok`.
//
// A gate that walks the tree fails on the ABSENCE of protection rather than on
// its presence. That inversion is the whole point, and it is why this lives in
// loop-runtime-ops instead of being a sixth copy of the ratchet.
//
// # What it refuses, and the three spellings that are the same thing
//
// Matching `Status().Update(` as text was measured evadable during the review of
// slice-27, by two-line rewrites that compile and persist status just the same:
//
//   - `sw := r.Status()` then `sw.Update(...)` — the receiver stops being a call
//   - `r.SubResource("status").Update(...)` — a different selector, one subresource
//   - the two combined
//
// So the walk resolves local aliases and treats `SubResource("status")` as the
// synonym it is. A fence that only recognises the spelling its author reached for
// reports clean because it is looking away.
//
// # What it deliberately does not judge
//
// `_test.go` files. A test legitimately seeds state with `Update`, and judging it
// would push authors to weaken the gate rather than the test. The Go toolchain's
// own rule is used: `_test.go` compiles solely into the test binary.
package statuswritercontract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Config parameterises one run.
type Config struct {
	// Root is the repository root. The walk is scoped to Root/operators.
	Root string
}

// Kind distinguishes the two ways the contract is broken. The remedy differs —
// one converts a write, the other adds a summary — so a single label would send
// the reader to the wrong fix.
type Kind int

const (
	// BareUpdate: status persisted with Update instead of a merge patch.
	BareUpdate Kind = iota
	// UncoveredSummary: status persisted in a package where nothing folds the
	// kstatus readiness summary, in-line or deferred.
	UncoveredSummary
)

// Violation is one production site that breaks the contract.
type Violation struct {
	Kind Kind
	File string
	Line int
	Func string
	// Form records HOW the status writer was reached, so the failure message
	// distinguishes a plain call from an alias or a SubResource spelling.
	Form string
}

// Outcome is the result of one run.
type Outcome struct {
	Pass         bool
	FilesScanned int
	Violations   []Violation
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Summary renders the one-line result the gate runner prints.
func (o Outcome) Summary() string {
	if o.Pass {
		return fmt.Sprintf("PASS — no bare Status().Update, and every package that persists status "+
			"folds a readiness summary (%d production file(s) scanned)", o.FilesScanned)
	}
	var updates, uncovered int
	for _, v := range o.Violations {
		if v.Kind == BareUpdate {
			updates++
		} else {
			uncovered++
		}
	}
	return fmt.Sprintf("FAIL — %d bare status Update(s), %d package(s) persisting without a readiness "+
		"summary, across %d production file(s) scanned", updates, uncovered, o.FilesScanned)
}

// ActionableFix is printed under a failure.
const ActionableFix = `Persist status with a merge patch taken off a base snapshot:

    base := obj.DeepCopy()          // right after the Get, BEFORE mutating .Status
    // ... mutate obj.Status ...
    r.Status().Patch(ctx, &obj, client.MergeFrom(base))

Update replays the whole object as read at the Get, so it races any concurrent
spec or metadata write and the loser's change disappears with no error. A merge
patch carries no resourceVersion and sends only what changed.

Contract: ADR 2026-08-12-status-writer-contract. Migration: B-085.`

// reachesStatus reports how an expression reaches a status subresource writer,
// or "" when it does not.
func reachesStatus(e ast.Expr) string {
	src := gotypes.ExprString(e)
	switch {
	case strings.Contains(src, ".Status()"):
		return "Status()"
	case strings.Contains(src, `.SubResource("status")`):
		return `SubResource("status")`
	}
	return ""
}

// fileScan is what one production file contributes to the package verdict.
type fileScan struct {
	violations []Violation
	// persists are every status write in the file, Update or Patch alike. The
	// summary question is about persistence, not about which verb reached it.
	persists []Violation
	// coversSummary is true when the file folds the summary in-line or defers one.
	// It is a PACKAGE-level property: the defer lives in Reconcile and covers the
	// helpers in sibling files, which is the shape seven controllers use.
	coversSummary bool
}

// scanFile collects what one parsed production file contributes.
func scanFile(fset *token.FileSet, parsed *ast.File, rel string) fileScan {
	var scan fileScan
	var out []Violation
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		// Locals bound to a status writer: `sw := r.Status()`.
		aliases := map[string]string{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				if via := reachesStatus(rhs); via != "" && i < len(assign.Lhs) {
					if id, ok := assign.Lhs[i].(*ast.Ident); ok {
						aliases[id.Name] = via + " via local `" + id.Name + "`"
					}
				}
			}
			return true
		})

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			// Either shape of summary fold marks the whole PACKAGE covered: the
			// deferred one lives in Reconcile and covers helpers in sibling files,
			// which is the shape seven controllers use.
			if sel.Sel.Name == "SetReadinessSummaryViaPkg" || sel.Sel.Name == "DeferReadinessSummary" {
				scan.coversSummary = true
				return true
			}
			if sel.Sel.Name != "Update" && sel.Sel.Name != "Patch" {
				return true
			}
			form := reachesStatus(sel.X)
			if form == "" {
				if id, ok := sel.X.(*ast.Ident); ok {
					form = aliases[id.Name]
				}
			}
			if form == "" {
				return true
			}
			v := Violation{
				File: rel, Line: fset.Position(call.Pos()).Line,
				Func: fn.Name.Name, Form: form,
			}
			// Every status write counts toward the summary question, Update or
			// Patch alike — the invariant is about persistence, not about which
			// verb reached it.
			scan.persists = append(scan.persists, v)
			if sel.Sel.Name == "Update" {
				v.Kind = BareUpdate
				out = append(out, v)
			}
			return true
		})
	}
	scan.violations = out
	return scan
}

// Check walks Root/operators and refuses every bare status Update in production.
func Check(cfg Config) (Outcome, error) {
	base := filepath.Join(cfg.Root, "operators")
	var out Outcome
	pkgPersists := map[string][]Violation{}
	pkgCovered := map[string]bool{}

	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// vendor and testdata are not ours to judge.
			if n := d.Name(); n == "vendor" || n == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return fmt.Errorf("parsing %s: %w", path, perr)
		}
		out.FilesScanned++
		rel, rerr := filepath.Rel(cfg.Root, path)
		if rerr != nil {
			rel = path
		}
		scan := scanFile(fset, parsed, rel)
		out.Violations = append(out.Violations, scan.violations...)

		// Summary coverage is a PACKAGE verdict, so accumulate and decide after
		// the walk: judging per file would fail every helper whose Reconcile
		// defers the summary from a sibling file.
		dir := filepath.Dir(path)
		pkgPersists[dir] = append(pkgPersists[dir], scan.persists...)
		if scan.coversSummary {
			pkgCovered[dir] = true
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("walking %s: %w", base, err)
	}

	// Scanning nothing is a failure, not a pass. "there is nothing to check" and
	// "everything is clean" are different facts, and reporting the second for the
	// first is how a gate goes quiet without anyone noticing.
	if out.FilesScanned == 0 {
		return out, fmt.Errorf("no production file found under %s — a gate that reads nothing "+
			"verifies nothing", base)
	}

	// A package that persists status and folds no summary anywhere leaves the Ready
	// condition at whatever the PREVIOUS pass computed — read as current, and read
	// precisely when something has just gone wrong.
	//
	// Only the FIRST persist of each uncovered package is reported: the fix is one
	// summary for the package, and N findings for one fix trains the reader to skim.
	for dir, persists := range pkgPersists {
		if pkgCovered[dir] || len(persists) == 0 {
			continue
		}
		v := persists[0]
		v.Kind = UncoveredSummary
		out.Violations = append(out.Violations, v)
	}

	sort.Slice(out.Violations, func(i, j int) bool {
		if out.Violations[i].File != out.Violations[j].File {
			return out.Violations[i].File < out.Violations[j].File
		}
		return out.Violations[i].Line < out.Violations[j].Line
	})
	out.Pass = len(out.Violations) == 0
	return out, nil
}
