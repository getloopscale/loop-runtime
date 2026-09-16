// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main — what the delivery-gate bypass suite asserted, and what replaced it.
//
// THIS FILE EXISTS BECAUSE THE TESTS DID NOT SURVIVE THEIR SUBJECT.
//
// `delivery_gate_bypass_test.go` held eight tests over `.claude/hooks/delivery-gate.sh`,
// a Claude Code PreToolUse hook that read a `git push` off the tool call and ran the
// quality gates before letting it through. They were good tests: each was written
// against a real bypass that had reached the remote, and the file named all four.
//
// Measured 2026-09-16, three facts that together retire them:
//
//  1. `.claude/hooks/hooks.json` declares NINE hooks and none of them is the delivery
//     gate. The mechanism was removed from the kit — this is not a file lost from one
//     machine, which is what the tests' Fatalf reported for eight days.
//  2. `3d4075ca3` (2026-09-08) removed `.claude/` from tracking, so even had the hook
//     survived, the tests could not run in any clone or on any runner.
//  3. The replacement is `.git/hooks/pre-push`, thirteen lines that `exec task
//     quality:gates`. Its interface is nothing like the hook's — it receives no
//     command to inspect — so the eight tests cannot be repointed, only rewritten.
//
// WHAT THEY ASSERTED, so the next person writing the equivalent starts here:
//
//   - a push is recognised through a working-directory flag (`git -C path push`)
//   - `--dry-run` ELSEWHERE in a compound command does not disarm the gate
//   - a push that is ITSELF a dry run is skipped
//   - what is not a push is ignored
//   - a push inside a subshell is still seen
//   - every segment of a compound command is examined, not only the first
//   - a command whose pushes are ALL dry runs is skipped
//   - the controls that must gate still gate
//
// WHAT THE REPLACEMENT'S ESCAPES ARE, stated because they are deliberate and a test
// over the new mechanism must encode them rather than discover them:
// `git push --no-verify` and `LOOPZERO_SKIP_PREPUSH=1`, both named in its own header.
//
// The gap this leaves, named rather than left to be found: nothing now asserts that
// the pre-push hook cannot be talked out of running by the SHAPE of a command. The
// old hook parsed one; the new one does not, so the class of bypass the eight tests
// covered cannot occur in the same form — but "cannot occur in that form" is a
// different claim from "is tested", and only the first is true today.
//
// THE FAMILY, swept rather than found one at a time.
//
// Four files and twenty-six tests reached the removed hook, and the first fix attempt
// found only the first of them. Swept by compiling the package instead:
//
//	delivery_gate_bypass_test.go    8 tests  — parsed a command for push bypasses
//	delivery_gate_identity_test.go  2 tests  — executed the hook against a stub tree
//	gate_armed_check_test.go       10 tests  — copied the hook byte for byte into a
//	                                           synthetic armed tree (`armedTree`)
//	quality_delivery_gate_covers_tests_test.go
//	                               6 tests  — asserted the hook runs one real task
//	                                           target covering this module, carries
//	                                           exactly one task invocation, blocks
//	                                           outside this repository, and is wired
//	                                           into the PreToolUse Bash hook
//
// The third is the one worth arguing about, because its subject — the Python check
// `.github/scripts/assert-delivery-gate-armed.py` — is alive and runs in CI. Its
// `armedTree` helper copies the REAL hook on purpose, and the reason is written in
// its own comment: "a stub would let this suite pass while the real file had a
// bypass in it, which is the failure being defended against wearing a test's
// clothes." With the hook gone there is nothing to copy, and a stub is exactly what
// that comment refuses.
//
// WHAT THIS COSTS, stated rather than discovered: premises 1-3 of that check — hook
// exists, hook is wired, hook has no bypass — are no longer covered by a Go test.
// What replaced the coverage is narrower and in a different language: the check now
// asks FIRST which regime the repository is in (`premise_0_ci_gates_the_pull_request`),
// and three canaries were run against it on 2026-09-16 — no `pull_request` trigger
// fails, a COMMENT mentioning `pull_request` does not fool it, a real trigger passes.
// That covers the path CI now takes and does not cover the path it no longer takes.
package main
