// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testskipabsentpath implements the `test-skip-absent-path` gate.
//
// # The defect
//
// A test guards itself with a filesystem probe and skips when the path is
// missing:
//
//	if _, err := os.Stat("../../../cli/src/index.ts"); os.IsNotExist(err) {
//	        t.Skipf("cli/ moved to the sibling repository")
//	}
//	// ... assertions that never run ...
//
// Conditional in form, unconditional in fact: when the path is not in this tree,
// the skip is the only branch ever taken, the package still reports `ok`, and
// everything below the guard is dead code that reads like a live assertion.
//
// # The verdicts
//
// For every path-guarded skip under the scanned trees (DefaultScanDirs), exactly
// one of:
//
//  1. CONDITIONAL — the guarded path exists here; the guard is genuinely
//     conditional.
//  2. ABSENT and undeclared — the skip is unconditional in fact. FAIL: remove the
//     test, or declare it in the pin file with a written reason.
//  3. TRACKED — an absence declared in the pin, printed on every run so it is
//     never quieter than a fresh one. A pin whose path came back also fails.
//  4. EXTERNAL — the path is rooted outside this repository ($HOME, an absolute
//     literal, a path climbing above the root). Reported as written and not
//     judged: it is a fact about a machine, not about this repository.
//
// A guard whose path cannot be read is UNRESOLVED and fails. There is no depth at
// which the gate goes quiet: it reads the answer or says it could not.
//
// # The floors
//
// A scanner that finds nothing must not call that clean. Every scan directory
// must exist and yield at least one _test.go file (per root), and the scan must
// find at least one skip call: any of these is SETUP BROKEN (exit 5). Every pin
// line must be re-found by the scan, or the gate FAILS (exit 1) naming it — a
// declaration nobody re-finds is a detector that stopped detecting.
//
// # How the guard is read
//
// The files are split by responsibility: scan.go walks the trees and parses each
// package directory; index.go builds the package-wide binding index (helpers,
// methods, func-typed fields, rebindings); guards.go walks each test body for
// skip sites and the guards over them; predicates.go reads a guard's condition,
// following same-package predicates; resolver.go resolves path expressions and
// the names bound to them; pins.go reads the declaration file.
//
// The probe may be inline or behind same-package predicates, methods, func
// values, locals, table entries and parenthesised calls, in either arm of the
// guard. A predicate is READ, not trusted: one that reaches a filesystem probe
// anywhere in its same-package delegation chain but whose answer cannot be
// followed is UNRESOLVED. Reachability is followed under a visited set and path
// resolution is bounded at maxResolveDepth hops; past the bound the guard is
// UNRESOLVED, never silent.
//
// # Limits, stated so they are not mistaken for coverage
//
// The package parses without type information, so names are resolved textually:
//
//   - A method is found by NAME. Two types declaring the same name make it
//     ambiguous, and an ambiguous method that touches the filesystem is
//     UNRESOLVED. A path held in the receiver's fields is UNRESOLVED.
//   - A struct field whose value the caller's scope does not spell, a table entry
//     under a key that cannot be read, and a package name rebound after its
//     declaration are UNRESOLVED — which value runs is dataflow, and this gate has
//     none.
//   - A local is followed only where statement order decides it. A name an `=`
//     reaches from a branch, a loop or a closure is UNRESOLVED rather than bound
//     to the last assignment the walk happened to pass.
//   - A func value the caller hands a helper that the helper then calls WITH
//     arguments is not followed; the zero-argument form is.
//   - A bool VALUE (`if !h.havePath { t.Skip() }`) has no body to read.
//   - A skip reached through a function value (`skip := t.Skip; skip()`) is
//     neither a site nor counted.
//   - A skip carried by a helper in another package is not read: the scope is one
//     directory at a time.
//   - Following a skip through helpers stops at maxDelegatedSkipDepth frames, and
//     past that bound the walk is silent: at the bound it is not yet known whether
//     a guard exists at all.
//
// A bare, unguarded skip is not this gate's subject; `test-skip-unconditional`
// fails one that carries statements below it. The two partition the class by
// guard position, and share one definition of what a skip call is (skipshape).
package testskipabsentpath
