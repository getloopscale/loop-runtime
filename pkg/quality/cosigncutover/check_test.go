// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cosigncutover

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupRoot(t *testing.T, gaDate, exception string) string {
	t.Helper()
	root := t.TempDir()
	gh := filepath.Join(root, ".github")
	if err := os.MkdirAll(gh, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if gaDate != "<absent>" {
		if err := os.WriteFile(filepath.Join(gh, "v1.0-ga-date.txt"), []byte(gaDate), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
	}
	if exception != "" {
		if err := os.WriteFile(filepath.Join(gh, "cosign-cutover-exception.txt"), []byte(exception), 0o644); err != nil {
			t.Fatalf("write exception: %v", err)
		}
	}
	return root
}

func asOf(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(dateLayout, s)
	if err != nil {
		t.Fatalf("asOf parse: %v", err)
	}
	return tm
}

// CASE 1 — pre-GA (no marker) → dormant, pass.
func TestPreGADormant(t *testing.T) {
	root := setupRoot(t, "<absent>", "")
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Dormant {
		t.Error("expected Dormant pre-GA")
	}
}

// CASE 2 — within budget (day 30).
func TestWithinBudgetDay30(t *testing.T) {
	root := setupRoot(t, "2026-06-01", "")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-07-01")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Days != 30 || o.Warn || o.Blocked {
		t.Errorf("day30: days=%d warn=%v blocked=%v", o.Days, o.Warn, o.Blocked)
	}
}

// CASE 3 — exact day 75 boundary → silent OK.
func TestDay75Boundary(t *testing.T) {
	root := setupRoot(t, "2026-06-01", "")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-08-15")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Days != 75 || o.Warn || o.Blocked {
		t.Errorf("day75: days=%d warn=%v blocked=%v", o.Days, o.Warn, o.Blocked)
	}
}

// CASE 4 — slip-alert window day 76 → warn, not blocked.
func TestSlipAlertDay76(t *testing.T) {
	root := setupRoot(t, "2026-06-01", "")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-08-16")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Days != 76 || !o.Warn || o.Blocked {
		t.Errorf("day76: days=%d warn=%v blocked=%v", o.Days, o.Warn, o.Blocked)
	}
}

// CASE 5 — exact day 90 → still warn window.
func TestDay90Boundary(t *testing.T) {
	root := setupRoot(t, "2026-06-01", "")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-08-30")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Days != 90 || !o.Warn || o.Blocked {
		t.Errorf("day90: days=%d warn=%v blocked=%v", o.Days, o.Warn, o.Blocked)
	}
}

// CASE 6 — day 91 without exception → blocked.
func TestDay91Block(t *testing.T) {
	root := setupRoot(t, "2026-06-01", "")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-08-31")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Days != 91 || !o.Blocked {
		t.Errorf("day91: days=%d blocked=%v", o.Days, o.Blocked)
	}
}

// CASE 7 — day 91 WITH signed exception → permitted.
func TestDay91WithSignedException(t *testing.T) {
	root := setupRoot(t, "2026-06-01",
		"slip-rationale: vendor outage\napproved-by: ops@example.com 2026-08-30\n")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-08-31")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Blocked || !o.ExceptionPermitted {
		t.Errorf("day91+exc: blocked=%v permitted=%v", o.Blocked, o.ExceptionPermitted)
	}
}

// CASE 8 — day 91 with UNSIGNED exception → still blocked.
func TestDay91UnsignedException(t *testing.T) {
	root := setupRoot(t, "2026-06-01", "slip-rationale: filed but never signed\n")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-08-31")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Blocked || o.ExceptionPermitted {
		t.Errorf("day91+unsigned: blocked=%v permitted=%v", o.Blocked, o.ExceptionPermitted)
	}
}

// CASE 9 — empty marker → error.
func TestEmptyMarker(t *testing.T) {
	root := setupRoot(t, "", "")
	if _, err := Check(Config{Root: root}); err == nil {
		t.Error("expected error for empty marker")
	}
}

// CASE 10 — garbage marker → error.
func TestGarbageMarker(t *testing.T) {
	root := setupRoot(t, "not-a-date", "")
	if _, err := Check(Config{Root: root}); err == nil {
		t.Error("expected error for garbage marker")
	}
}

// Extra — marker with trailing content uses only the first line.
func TestMarkerFirstLineOnly(t *testing.T) {
	root := setupRoot(t, "2026-06-01\nsome trailing note\n", "")
	o, err := Check(Config{Root: root, AsOf: asOf(t, "2026-07-01")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.GADate != "2026-06-01" || o.Days != 30 {
		t.Errorf("first-line: gaDate=%q days=%d", o.GADate, o.Days)
	}
}

// Extra — zero AsOf falls back to time.Now (smoke: a far-past GA blocks).
func TestZeroAsOfUsesNow(t *testing.T) {
	root := setupRoot(t, "2000-01-01", "")
	o, err := Check(Config{Root: root}) // AsOf zero → now; year 2000 GA is >90d
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Blocked {
		t.Errorf("expected block for ancient GA date, got days=%d", o.Days)
	}
}
