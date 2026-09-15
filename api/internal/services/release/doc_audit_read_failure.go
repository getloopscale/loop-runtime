// Package release — audit-trail read failures.
//
// THIS FILE CARRIES EVIDENCE, NOT BEHAVIOUR. It exists because the three tests
// that pin the contract below were deleted on 2026-09-15 and their only
// surviving copies were untracked — a clone of this repository received none of
// them. What follows is enough to rebuild the failing tests from a tracked
// source.
//
// The deleted file was release_inspector_b046_test.go. It was never committed
// (0 commits in history) and was removed because its name carried a ticket
// number, which the engineering principles forbid: a name must say what the
// thing does, and a ticket id outlives the tracker it points at. The removal
// was correct; the evidence loss it caused is what this file repays.
//
// Three properties are unimplemented and must fail before they pass:
//
//  1. A failure READING the audit trail is observable at maximum verbosity.
//     Today it can be swallowed, so an operator raising verbosity to diagnose
//     sees the same output as one who did not.
//
//  2. A read failure is DISTINGUISHABLE from a genuinely empty trail. This is
//     the defect's whole shape: "I could not read it" and "there is nothing to
//     read" are different facts, and a caller that cannot tell them apart will
//     report an absence of findings as evidence of absence.
//
//  3. Truncation of the audit window is SIGNALLED rather than silently applied.
//     A truncated answer that does not say it was truncated is a wrong answer
//     with the shape of a right one.
//
// Name the tests for those properties. Do not restore the deleted names.
package release
