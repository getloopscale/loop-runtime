// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package verdict is the one contract every quality gate's check satisfies.
//
// A check keeps its own outcome type — the violations, counts and reasons it
// measured are what its tests assert and what its report prints — and that
// type states, through Verdict, which of four verdicts the outcome amounts to.
// The rule deciding the verdict lives next to the data it reads, in the
// check's own package, and is tested there.
//
// The error a check returns means one thing only: it could not measure. A
// violation is never an error; it is a result whose Verdict is Fail. Before
// this contract, 42 checks reported violations as sentinel errors and every
// caller listed those sentinels by hand to tell "found something wrong" from
// "could not look", which is how a new sentinel became an exit 5 instead of a
// finding.
//
// The exit status each verdict produces is decided once, by the quality
// runner (internal/qualitycli), and nowhere else.
package verdict

// Verdict is what a check's outcome amounts to.
//
// The zero value is not a verdict: an outcome that never decided must not read
// as a pass. The runner reports it as a check that could not complete.
type Verdict int

const (
	unset Verdict = iota
	// Pass — the check ran over its subject and found nothing wrong. A pass
	// that covered less than the whole subject says so in its report; the
	// verdict is still Pass.
	Pass
	// Fail — the check ran and found a violation.
	Fail
	// Skip — the check did not evaluate its rule here, for a reason its policy
	// accepts and its report states: the subject is legitimately absent from
	// this checkout, or an advisory gate's tool is not installed. It exits 0,
	// and is never used for a subject that exists but could not be read.
	Skip
	// NotVerified — the check has a subject it could not verify on this
	// machine (a gitignored tree that is not installed, a live directory that
	// is absent) and has already said why. It exits Incomplete, apart from
	// both a pass and a violation.
	NotVerified
)

// String names the verdict the way reports spell it.
func (v Verdict) String() string {
	switch v {
	case Pass:
		return "PASS"
	case Fail:
		return "FAIL"
	case Skip:
		return "SKIP"
	case NotVerified:
		return "NOT VERIFIED"
	}
	return "NO VERDICT"
}

// Result is what a gate's check returns: its own outcome, which states the
// verdict it amounts to.
//
// Verdict deliberately does not implement Result. A caller cannot hand the
// runner a verdict it chose; it hands over the outcome the check produced.
type Result interface {
	Verdict() Verdict
}

// Of returns Pass when a check found no violations and Fail when it found
// any. It is the rule most checks apply; a check whose rule differs states
// its own.
func Of[T any](violations []T) Verdict {
	if len(violations) == 0 {
		return Pass
	}
	return Fail
}

// FromPass returns Pass for true and Fail for false, for an outcome that
// already records its decision as a boolean.
func FromPass(pass bool) Verdict {
	if pass {
		return Pass
	}
	return Fail
}
