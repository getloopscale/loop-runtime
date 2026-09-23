// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package calendarday

import (
	"testing"
	"time"
)

var brt = time.FixedZone("-03", -3*3600)

// 21:15 on 2026-08-06 in -03 is already 2026-08-07 in UTC; the calendar day
// a person reads there is still the 6th.
func TestOf_KeepsTheDayTheClockShowedWhereItRan(t *testing.T) {
	got := Of(time.Date(2026, 8, 6, 21, 15, 0, 0, brt))
	if want, _ := Parse("2026-08-06"); !got.Equal(want) {
		t.Fatalf("want 2026-08-06, got %s", got)
	}
}

func TestToday_UsesTheInjectedInstant(t *testing.T) {
	got := Today(time.Date(2026, 8, 6, 23, 59, 0, 0, brt))
	if want, _ := Parse("2026-08-06"); !got.Equal(want) {
		t.Fatalf("want 2026-08-06, got %s", got)
	}
}

func TestDaysBetween_CountsCalendarDaysNotElapsedHours(t *testing.T) {
	start, _ := Parse("2026-08-06")
	if d := DaysBetween(start, time.Date(2026, 8, 6, 21, 15, 0, 0, brt)); d != 0 {
		t.Fatalf("same calendar day must be 0 days, got %d", d)
	}
	if d := DaysBetween(start, time.Date(2026, 8, 7, 0, 5, 0, 0, brt)); d != 1 {
		t.Fatalf("next calendar day must be 1 day, got %d", d)
	}
}
