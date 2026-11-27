// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deadlineratchet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A hand-written marker date is a calendar day. At 21:15 in -03 it is still
// that day for the person who wrote it, even though UTC has moved on; counting
// elapsed UTC hours reported one day too many.
func TestDaysAreCalendarDaysInTheLocalClock(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, cosignSpec.MarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("2026-08-06\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evening := time.Date(2026, 8, 6, 21, 15, 0, 0, time.FixedZone("-03", -3*3600))
	o, err := Check(cosignSpec, Config{Root: root, AsOf: evening})
	if err != nil {
		t.Fatal(err)
	}
	if o.Days != 0 {
		t.Fatalf("want 0 days on the marker's own calendar day, got %d", o.Days)
	}
}
