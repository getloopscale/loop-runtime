// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package skipshape is the one definition of a test skip shared by the two gates
// that partition the dead-assertion class: test-skip-absent-path (a skip under a
// filesystem guard) and test-skip-unconditional (a skip that is a direct
// statement of a body). Two gates that disagree about what a skip is leave a
// spelling that neither of them sees — which is what happened while each kept
// its own copy: one was widened to any receiver and the other still saw only a
// bare identifier, so `h.t.Skip()` followed by dead assertions was invisible to
// test-skip-unconditional.
package skipshape

import "go/ast"

// DefaultScanDirs are the trees both gates govern, repo-relative. They scan the
// same trees because the class is one class in two spellings: a tree one gate
// reads and the other does not is a tree where the cheaper spelling goes
// unguarded. cmd/loopzero-ops is included so the gates also govern the tests of the
// gates themselves.
var DefaultScanDirs = []string{"infra/tests", "api", "cmd/loopzero-ops"}

// IsSkipCall reports whether n is `<expr>.Skip(...)`, `<expr>.Skipf(...)` or
// `<expr>.SkipNow()`.
//
// The receiver is any expression: `t.Skip()`, `h.t.Skip()` and `s.T().Skip()`
// are all testing.TB's in a _test.go file. It is not type-checked — both gates
// parse a tree they do not build, without type information.
func IsSkipCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Skip", "Skipf", "SkipNow":
		return true
	}
	return false
}
