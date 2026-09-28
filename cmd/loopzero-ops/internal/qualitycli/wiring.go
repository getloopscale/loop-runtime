// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"strings"
)

// Wiring says why a registered gate is NOT invoked from taskfiles/quality.yml.
//
// A gate can be implemented, registered, unit-tested — and never actually run
// (issue #255: 39 of 73 gates were in that state). Every gate is therefore
// EITHER wired into taskfiles/quality.yml OR carries a Wiring exemption on its
// own spec, stating why not. The zero value means "wired"; the decision lives
// next to the gate, not in a separate ledger that has to be edited in step.
//
// It is a value object rather than a bare string because one category carries
// an invariant: operatorRun asserts the gate depends on state OUTSIDE the
// repository tree, and that claim was accepted on trust while it was a string —
// two entries turned out to be hermetic comparisons. The invariant is a
// DECLARATION: an operatorRun exemption must NAME its external input, and
// TestOperatorRun_AbsentableInputsAreProven runs the gate without it.
type Wiring struct {
	category      string
	externalInput string // required when category == categoryOperatorRun
	// ambient marks external state that is always present (the wall clock),
	// and therefore cannot be proven by running the gate without it.
	ambient bool
}

// Exempt reports whether the gate is declared as not wired.
func (w Wiring) Exempt() bool { return w.category != "" }

// OperatorRun reports whether the exemption claims an external input.
func (w Wiring) OperatorRun() bool { return w.category == categoryOperatorRun }

// Ambient reports whether that external input is always present.
func (w Wiring) Ambient() bool { return w.ambient }

// ExternalInput is the state an operator-run gate depends on.
func (w Wiring) ExternalInput() string { return w.externalInput }

// String keeps the exemption readable in test messages.
func (w Wiring) String() string {
	if w.externalInput == "" {
		return w.category
	}
	return w.category + " [external input: " + w.externalInput + "]"
}

// Validate enforces the one invariant that gives operatorRun its meaning.
func (w Wiring) Validate() error {
	if w.category != categoryOperatorRun {
		return nil
	}
	if strings.TrimSpace(w.externalInput) == "" {
		return ErrUnnamedExternalInput
	}
	return nil
}

// ErrUnnamedExternalInput is returned when an operatorRun exemption does not
// say WHICH external state the gate needs.
var ErrUnnamedExternalInput = errors.New(
	"classified operator-run but names no external input — an operator-run gate depends on state outside the repo tree, so say which " +
		"(a cluster, an env var, an artefact from a previous run). If nothing can be named, the gate is hermetic and this is the wrong category")

const categoryOperatorRun = "operator-run — needs live cluster state / a ceremony / a cadence, not a hermetic repo scan"

// operatorRunOn declares an absent-able external input (a file or an
// environment variable that can be missing), which makes the claim testable:
// run the gate with the input gone, and a gate that truly depends on it cannot
// reach a verdict.
func operatorRunOn(externalInput string) Wiring {
	return Wiring{category: categoryOperatorRun, externalInput: externalInput}
}

// operatorRunOnAmbient declares external state that is ALWAYS present and so
// cannot be proven by absence — the wall-clock date being the only instance.
// Naming it separately keeps unprovable claims from hiding among provable ones.
func operatorRunOnAmbient(externalInput string) Wiring {
	return Wiring{category: categoryOperatorRun, externalInput: externalInput, ambient: true}
}

var (
	// parameterized: requires mandatory flags, so a bare invocation from a
	// task would always fail. Invoked by a caller that supplies them.
	parameterized = Wiring{category: "parameterized — takes mandatory flags; a bare run from quality:gates would fail"}

	// utility: produces input for other gates rather than a verdict.
	utility = Wiring{category: "utility — emits data consumed by other gates, has no pass/fail verdict of its own"}

	// failingGate: runs, and reports a REAL finding today. The gate is right
	// and the tree is wrong; wiring it before the finding is addressed would
	// turn quality:gates red for reasons unrelated to the change under test.
	failingGate = Wiring{category: "TODO(#255) — WORKS, and currently FAILS on a real finding; fix the finding, then wire"}

	// brokenGate: measured to produce a wrong verdict today. Distinct from
	// failingGate on purpose — "the tree is wrong" and "the gate is wrong"
	// call for different fixes.
	brokenGate = Wiring{category: "TODO(#255) — BROKEN: produces a wrong verdict; must be repaired before it can be wired"}
)

// wiringCategories is every category, for the test that holds each to its
// invariant. slowGate/vacuousGate/unwiredDebt had no member and were removed:
// a category nobody can be filed under is vocabulary nobody reads.
var wiringCategories = map[string]Wiring{
	"parameterized": parameterized,
	"utility":       utility,
	"failingGate":   failingGate,
	"brokenGate":    brokenGate,
}
