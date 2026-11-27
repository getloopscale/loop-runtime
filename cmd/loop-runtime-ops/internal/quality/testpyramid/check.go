// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package testpyramid gates the test pyramid — by DERIVING it from the tree.
//
// The ADR D7 targets (unit ≥70%, integration ≤25%, e2e ≤5%) still live here as
// `DefaultThresholds`, but today they are REPORTED, not enforced: see derived.go
// for the measured reason.
//
// # What was removed, and why
//
// This package used to read three percentages written in `infra/quality/test-inventory.json`.
// Measured on 2026-08-17 (B-106): the file described 375 of 988 test files —
// **62% blind** — and declared 99.4% unit over that sample, which excludes the entire
// `infra/tests/e2e` suite. A file could declare whatever pyramid it wanted.
//
// Reading the declared file went away together with the file. What remains is `Thresholds`
// (the ADR target) and the ratchet over the measurement, in derived.go.
package testpyramid

// Thresholds are the pyramid bounds (percentages).
type Thresholds struct {
	UnitMinPct        float64
	IntegrationMaxPct float64
}

// DefaultThresholds per ADR D7.
func DefaultThresholds() Thresholds {
	return Thresholds{UnitMinPct: 70, IntegrationMaxPct: 25}
}
