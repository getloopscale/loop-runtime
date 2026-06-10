// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import "time"

// CurrentPeriod returns the start and end of the current calendar month
// (UTC). Period boundary is minute 0 of day 1 — the aggregator schedules
// counter reset at that boundary per plan EC1.9.2.
//
// Returns unix seconds for both bounds.
func CurrentPeriod(now time.Time) (start, end int64) {
	utc := now.UTC()
	startT := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	endT := startT.AddDate(0, 1, 0)
	return startT.Unix(), endT.Unix()
}

// formatPeriodImpl is the helper invoked by formatPeriod() in cost_cap.go.
// Kept in its own file so the cost_cap.go file body stays focused on the
// enforcement logic without time-format scaffolding.
func formatPeriodImpl(unixSec int64) string {
	return time.Unix(unixSec, 0).UTC().Format("2006-01")
}
