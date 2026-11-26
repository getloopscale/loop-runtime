// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package planregression groups suites that keep completed infrastructure
// work from being undone — not permanent platform invariants.
//
// They are deliberately not quality gates: a gate in the registry is a
// permanent invariant that `loopruntime-ops quality --list` enumerates, and these
// suites protect specific outcomes (how the platform is installed, what the
// dev-loop docs say, how OpenCost is wired). They run under `go test ./...`
// as regression tests.
package planregression
