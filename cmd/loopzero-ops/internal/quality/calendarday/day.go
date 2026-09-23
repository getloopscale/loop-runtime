// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package calendarday compares hand-written YYYY-MM-DD dates with "today".
//
// The dates in allowlists and deadline markers are written by a person reading
// their own calendar. Comparing one against a UTC clock makes it expire a day
// early for the last hours of every day in any negative offset (measured
// 2026-08-06 21:15 -03, when two readers of one file disagreed about what
// "today" was). Every value this package returns is a civil day: the calendar
// day a clock showed where it ran, carried as midnight UTC so two days compare
// and subtract exactly.
package calendarday

import "time"

// Layout is the hand-written date format.
const Layout = "2006-01-02"

// Of returns the calendar day t falls on in its own location.
func Of(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Today returns the local calendar day, or the calendar day of injected when
// it is non-zero (tests and --as-of flags).
func Today(injected time.Time) time.Time {
	if !injected.IsZero() {
		return Of(injected)
	}
	return Of(time.Now())
}

// Parse reads a hand-written YYYY-MM-DD as a calendar day.
func Parse(s string) (time.Time, error) {
	return time.Parse(Layout, s)
}

// DaysBetween returns the whole calendar days from a to b.
func DaysBetween(a, b time.Time) int {
	return int(Of(b).Sub(Of(a)).Hours() / 24)
}
