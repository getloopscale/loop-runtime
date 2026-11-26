// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package exitcode is loopruntime-ops' one exit-status table. Every subcommand —
// quality gates, seeds, chaos, drill, pulumi, leak — returns through it, so a
// caller can read an exit status without knowing which command produced it.
//
// It uses the repository's permanent vocabulary (CLAUDE.md Rule 4: 0 success,
// 1 usage, 5 partial failure) and maps loopruntime-ops' outcomes onto it:
//
//	0  OK          the check passed, or the operation reached its goal
//	1  Usage       invalid invocation (unknown command, bad flag); nothing ran
//	1  Failure     it ran and found a violation, or the operation failed
//	5  Incomplete  it could not reach a verdict (an input was unreadable, a
//	               tool errored, the target was unreachable)
//
// Usage and Failure share a value on purpose: both mean "no success, and the
// output says why", which is all a shell caller branches on. What a caller
// must be able to tell apart is Failure (the subject is wrong) from
// Incomplete (nothing was measured), and those never share a value.
package exitcode

const (
	// OK — the check passed, or the operation reached its goal.
	OK = 0
	// Usage — invalid invocation; nothing ran.
	Usage = 1
	// Failure — it ran, and the subject is wrong (a violation, a failed
	// seed, a breached SLO).
	Failure = 1
	// Incomplete — it could not reach a verdict.
	Incomplete = 5
)
