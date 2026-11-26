// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dbconv

import (
	"testing"
	"time"
)

func TestStringEmptyToNilPtr(t *testing.T) {
	if got := StringEmptyToNilPtr(""); got != nil {
		t.Errorf("empty string must map to nil, got %v", *got)
	}
	got := StringEmptyToNilPtr("x")
	if got == nil || *got != "x" {
		t.Errorf("non-empty string must map to &s, got %v", got)
	}
}

func TestTimeToTimestamptz(t *testing.T) {
	now := time.Now()
	ts := TimeToTimestamptz(now)
	if !ts.Valid || !ts.Time.Equal(now) {
		t.Errorf("TimeToTimestamptz(%v) = {Time:%v Valid:%v}, want valid + equal", now, ts.Time, ts.Valid)
	}
}

func TestTimePtrToTimestamptz(t *testing.T) {
	if ts := TimePtrToTimestamptz(nil); ts.Valid {
		t.Error("nil *time.Time must map to Valid=false")
	}
	now := time.Now()
	ts := TimePtrToTimestamptz(&now)
	if !ts.Valid || !ts.Time.Equal(now) {
		t.Errorf("TimePtrToTimestamptz(&now) = {Time:%v Valid:%v}, want valid + equal", ts.Time, ts.Valid)
	}
}
