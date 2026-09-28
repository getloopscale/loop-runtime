// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The zero value keeps the seed's own default, which is the contract `Policy`
// already established one field above in Deps: nil/zero preserves the behaviour
// that existed before the field.
//
// It takes the default as an ARGUMENT rather than owning one, because the two
// defaults differ ON PURPOSE — loopzero-api waits 120s, coredns 60s. A single
// package-wide default would silently double the coredns wait, which is a
// production behaviour change smuggled in under a test speed-up.
func TestDeps_SettleTimeout_ZeroKeepsTheSeedsOwnDefault(t *testing.T) {
	if got := (Deps{}).settleTimeout(90 * time.Second); got != 90*time.Second {
		t.Errorf("zero must defer to the caller's default; got %v", got)
	}
	if got := (Deps{RolloutSettleTimeout: -1}).settleTimeout(90 * time.Second); got != 90*time.Second {
		t.Errorf("a negative is not an instruction, it is a mistake — defer to the default; got %v", got)
	}
}

// And an injected value wins, which is the whole point: the composition root
// decides, the seed does not.
func TestDeps_SettleTimeout_InjectedWins(t *testing.T) {
	if got := (Deps{RolloutSettleTimeout: 5 * time.Millisecond}).settleTimeout(120 * time.Second); got != 5*time.Millisecond {
		t.Errorf("the injected policy must win over the seed's default; got %v", got)
	}
}

// Every call site of the wait helper resolves its budget through Deps.
//
// The first version of this assertion was a regex, and the review broke it four
// ways. The realistic break is the one a seventh seed would write by accident:
//
//	waitForRolloutSettled(ctx, cli, ns, deployName("loopzero-api"), 120*time.Second)
//
// `[^)]*` stopped at the `)` of deployName, so the literal after it was never
// seen — the guard went quiet exactly when a caller stopped passing plain
// strings. It also missed `time.Duration(120)*time.Second` and a duration
// hoisted into a local variable. A regex reads text; what has to be checked is
// the SHAPE of the last argument, so the check reads the tree instead.
//
// Two things are required, and the second is why the first is not enough:
//
//  1. the last argument is a call to settleTimeout — so the environment, not the
//     call site, gets the final say;
//  2. its own argument is a NAMED identifier — because `d.settleTimeout(120*time.Second)`
//     is the same defect wearing the fix as a costume.
func TestWaitForRolloutSettled_EveryCallSiteResolvesThroughDeps(t *testing.T) {
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = file
	}

	var offenders []string
	calls := 0
	for name, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "waitForRolloutSettled" || len(call.Args) == 0 {
				return true
			}
			calls++
			where := fmt.Sprintf("%s:%d", filepath.Base(name), fset.Position(call.Pos()).Line)

			last := call.Args[len(call.Args)-1]
			inner, ok := last.(*ast.CallExpr)
			if !ok {
				offenders = append(offenders, where+" — the budget is decided at the call site; pass d.settleTimeout(<const>)")
				return true
			}
			sel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "settleTimeout" {
				offenders = append(offenders, where+" — the last argument is not settleTimeout, so Deps cannot override it")
				return true
			}
			// (2) — the default itself must be named, not spelled out here.
			if len(inner.Args) != 1 {
				offenders = append(offenders, where+" — settleTimeout takes exactly the seed default")
				return true
			}
			if _, named := inner.Args[0].(*ast.Ident); !named {
				offenders = append(offenders, where+" — settleTimeout's default must be a NAMED constant; a literal here is the same defect in a costume")
			}
			return true
		})
	}

	// A guard that inspected nothing cannot vouch for anything — and this one
	// walks a directory, so an empty parse would look identical to a clean tree.
	if calls == 0 {
		t.Fatal("no call to waitForRolloutSettled was found — the assertion measured nothing")
	}
	if len(offenders) > 0 {
		t.Errorf("the wait budget is policy of the environment, not of the call site (%d call(s) checked):\n  %s",
			calls, strings.Join(offenders, "\n  "))
	}
}
