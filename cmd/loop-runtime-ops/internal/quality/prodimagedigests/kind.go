// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package prodimagedigests

import "fmt"

// Kind names a population of violation.
//
// # Why the gate needs this and one boolean was not enough
//
// `EnforceMode bool` decided for every finding at once, and the findings are not
// one population. Measured 2026-08-17 on the real tree — 10 violations, two
// groups with different owners and different feasibility:
//
//   - 3 literal `:tag` references in Helm test pods. Third-party images at a
//     pinned version; the person who writes the chart fixes them, and the fix
//     costs one line and does not age (the version does not move, so neither
//     does the digest).
//   - 7 charts whose `values.yaml` declares no `image.digest`. The template is
//     already correct — it defers to `loop-runtime-common.image`, which prefers a digest
//     and falls back to the tag. What is missing is the VALUE, and the gate's own
//     message says who owns it (translated from the original Portuguese): *"the
//     image publisher is who produces it — a digest pinned by hand becomes a lie
//     at the next publication"*.
//
// One flag over both meant the only two settings were "fail on seven things
// nobody can fix today" and "warn on everything" — and the second is what shipped,
// for three months against a declared 7-day soak. The cost was not the warn; it
// was that three FIXABLE findings stayed invisible inside a number dominated by
// seven that were not.
//
// Naming the kinds does not add policy to the gate. The policy was already there,
// inside the boolean; this makes it possible to say which half is holding it.
type Kind string

const (
	// KindLiteralTag is a hardcoded `image: repo/name:tag` in a chart template.
	KindLiteralTag Kind = "literal_tag"
	// KindMissingDigest is a chart that defers to the canonical helper but
	// declares no `image.digest`, so the effective default install is mutable.
	KindMissingDigest Kind = "missing_digest"
)

// Violation is one finding, classified.
type Violation struct {
	Kind Kind
	File string
	// Line is 0 for a file-level finding (the values.yaml layer judges the file,
	// not a line).
	Line int
	Text string
}

// String renders the finding the way the operator reads it.
//
// The type changed from a plain string, and this method is what keeps the only
// output of this gate intact across that change. Without it the report would
// print Go struct syntax while every unit test stayed green — a gate whose
// findings nobody can read is a gate that is not consulted.
func (v Violation) String() string {
	if v.Line == 0 {
		return fmt.Sprintf("%s: %s", v.File, v.Text)
	}
	return fmt.Sprintf("%s:%d: %s", v.File, v.Line, v.Text)
}

// DefaultEnforce is the policy decided in
// MEET-ASYNC-AMENDMENT-2026-08-17-digest-enforcement-has-two-populations:
// enforce what the chart author can fix today, warn on what waits for the image
// publisher.
//
// Enforcing everything today would make the gate refuse seven findings whose fix
// does not exist yet — and a gate that cannot go green is switched off or worked
// around within a week, taking the protection it did give with it.
func DefaultEnforce() map[Kind]bool {
	return map[Kind]bool{KindLiteralTag: true}
}

// EnforceAll enforces every kind. This is what `--enforce` selects, and it is the
// end state: once the publisher emits `image.digest`, it becomes the default.
func EnforceAll() map[Kind]bool {
	return map[Kind]bool{KindLiteralTag: true, KindMissingDigest: true}
}
