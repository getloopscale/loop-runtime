// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
// asked for this gate in the same breath, and for a while it did not exist:
//
//	um gate estático recusa `Status().Update` novo nos operators —
//	sem ele a migração é reversível por descuido
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
// loopzero-ops instead of being a sixth copy of the ratchet.
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
)

// Config parameterises one run.
type Config struct {
	// Root is the repository root. The walk is scoped to Root/operators.
	Root string
}

// Violation is one production site that persists status with Update.
type Violation struct {
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

// Summary renders the one-line result the gate runner prints.
func (o Outcome) Summary() string {
	if o.Pass {
		return fmt.Sprintf("PASS — no bare Status().Update in operators (%d production file(s) scanned)",
			o.FilesScanned)
	}
	return fmt.Sprintf("FAIL — %d bare status Update(s) across %d production file(s) scanned",
		len(o.Violations), o.FilesScanned)
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

// scanFile collects the violations of one parsed production file.
func scanFile(fset *token.FileSet, parsed *ast.File, rel string) []Violation {
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
			if !ok || sel.Sel.Name != "Update" {
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
			out = append(out, Violation{
				File: rel, Line: fset.Position(call.Pos()).Line,
				Func: fn.Name.Name, Form: form,
			})
			return true
		})
	}
	return out
}

// Check walks Root/operators and refuses every bare status Update in production.
func Check(cfg Config) (Outcome, error) {
	base := filepath.Join(cfg.Root, "operators")
	var out Outcome

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
		out.Violations = append(out.Violations, scanFile(fset, parsed, rel)...)
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

	sort.Slice(out.Violations, func(i, j int) bool {
		if out.Violations[i].File != out.Violations[j].File {
			return out.Violations[i].File < out.Violations[j].File
		}
		return out.Violations[i].Line < out.Violations[j].Line
	})
	out.Pass = len(out.Violations) == 0
	return out, nil
}
