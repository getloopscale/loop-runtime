// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// seedsRequiringTheirNamespace are the seeds that write a Secret into a namespace
// they cannot assume exists, and must therefore ensure it first.
//
// The list is EXPLICIT rather than derived, and that is the point. Measured on
// 2026-08-14: seven seeds in this package write Secrets and only ONE ensured its
// namespace. Deriving "every seed that writes a Secret must ensure" would have
// been a guess about the other five — some of their namespaces may be guaranteed
// by the position of the seed in the chain, and asserting otherwise would fail
// them for a problem nobody measured.
//
// So this holds the ones with a decision behind them. Adding a row is how a seed
// joins the contract; the five absent ones are registered as an open question,
// not as an exemption.
var seedsRequiringTheirNamespace = map[string]string{
	"github_app_dev.go": "B-021 — ran before loopzero-system existed on a fresh cleanroom, the webhook " +
		"secret was never written, and the endpoint 404'd with no error anywhere in our logs",
	"broker.go": "ensured its namespace since it was written",
}

// TestSeedsEnsureTheirNamespaceBeforeWriting is a structural assertion, and it is
// structural for a measured reason: ensureNamespace returns nil against the fake
// client the unit tests use (see broker_helpers.go), so no in-package behavioural
// test can observe whether it was called. Asserting the call site is what is
// actually available — and it still catches the failure mode, which is somebody
// deleting the line.
func TestSeedsEnsureTheirNamespaceBeforeWriting(t *testing.T) {
	for file, why := range seedsRequiringTheirNamespace {
		t.Run(file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Clean(file))
			if err != nil {
				t.Fatalf("reading %s: %v", file, err)
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, file, src, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", file, err)
			}

			var ensures, writes []int
			ast.Inspect(parsed, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				line := fset.Position(call.Pos()).Line
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					if fn.Name == "ensureNamespace" {
						ensures = append(ensures, line)
					}
				case *ast.SelectorExpr:
					if fn.Sel.Name == "CreateOrUpdateSecret" {
						writes = append(writes, line)
					}
				}
				return true
			})

			if len(writes) == 0 {
				t.Fatalf("%s writes no Secret — it no longer belongs in this contract, so remove its "+
					"row rather than leaving an assertion that verifies nothing", file)
			}
			if len(ensures) == 0 {
				t.Fatalf("%s writes a Secret at line %d without ensuring its namespace anywhere.\n"+
					"Why this file is in the contract: %s", file, writes[0], why)
			}

			// Ordering is asserted only when the file has exactly one of each, and
			// that restriction came from being wrong: the first version compared the
			// earliest ensure against the earliest write globally and failed
			// broker.go, which writes to TWO clusters — mgmt first, then the workload
			// namespace it ensures. Per pair the order is right; measured across the
			// file it looked inverted.
			//
			// Tracking the client and namespace of each call would express the real
			// invariant, but both arrive as identifiers whose values live elsewhere,
			// so the AST alone cannot resolve which write belongs to which ensure.
			// Claiming the ordering anyway would be the same defect this contract
			// exists to catch, one level up. Presence still catches the failure mode
			// that produced B-021: somebody deleting the line.
			if len(ensures) == 1 && len(writes) == 1 && ensures[0] > writes[0] {
				t.Errorf("%s ensures its namespace at line %d, AFTER writing its only Secret at line %d "+
					"— with a single pair the order is the whole guarantee", file, ensures[0], writes[0])
			}
		})
	}
}
