// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deadlineratchet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// cosignSpec mirrors the §6.B.4 cosign cutover gate (warn 75, deadline 90).
var cosignSpec = Spec{
	MarkerRelPath:    ".github/v1.0-ga-date.txt",
	ExceptionRelPath: ".github/cosign-cutover-exception.txt",
	WarnDay:          75,
	DeadlineDay:      90,
}

// authSpec mirrors the §6.B.5 service-auth rotation gate (warn 60, deadline 90).
var authSpec = Spec{
	MarkerRelPath:    ".github/service-auth-ed25519-rotation-date.txt",
	ExceptionRelPath: ".github/service-auth-rotation-exception.txt",
	WarnDay:          60,
	DeadlineDay:      90,
}

func setup(t *testing.T, spec Spec, date, exception string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if date != "<absent>" {
		if err := os.WriteFile(filepath.Join(root, spec.MarkerRelPath), []byte(date), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
	}
	if exception != "" {
		if err := os.WriteFile(filepath.Join(root, spec.ExceptionRelPath), []byte(exception), 0o644); err != nil {
			t.Fatalf("write exception: %v", err)
		}
	}
	return root
}

func at(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(DateLayout, s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tm
}

func TestDormant(t *testing.T) {
	for _, spec := range []Spec{cosignSpec, authSpec} {
		root := setup(t, spec, "<absent>", "")
		o, err := Check(spec, Config{Root: root})
		if err != nil || !o.Dormant {
			t.Errorf("spec %s: expected dormant, err=%v dormant=%v", spec.MarkerRelPath, err, o)
		}
	}
}

func TestWithinBudget(t *testing.T) {
	for _, spec := range []Spec{cosignSpec, authSpec} {
		root := setup(t, spec, "2026-06-01", "")
		o, err := Check(spec, Config{Root: root, AsOf: at(t, "2026-07-01")})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if o.Days != 30 || o.Warn || o.Blocked {
			t.Errorf("spec %s day30: days=%d warn=%v blocked=%v", spec.MarkerRelPath, o.Days, o.Warn, o.Blocked)
		}
	}
}

// Warn boundary differs: cosign warns at 76, auth at 61.
func TestWarnBoundaryCosign(t *testing.T) {
	root := setup(t, cosignSpec, "2026-06-01", "")
	day75, _ := Check(cosignSpec, Config{Root: root, AsOf: at(t, "2026-08-15")})
	if day75.Warn {
		t.Error("cosign day75 should be in-budget, not warn")
	}
	day76, _ := Check(cosignSpec, Config{Root: root, AsOf: at(t, "2026-08-16")})
	if !day76.Warn || day76.Blocked {
		t.Errorf("cosign day76 should warn: %+v", day76)
	}
}

func TestWarnBoundaryAuth(t *testing.T) {
	root := setup(t, authSpec, "2026-06-01", "")
	day60, _ := Check(authSpec, Config{Root: root, AsOf: at(t, "2026-07-31")})
	if day60.Warn {
		t.Error("auth day60 should be in-budget, not warn")
	}
	day61, _ := Check(authSpec, Config{Root: root, AsOf: at(t, "2026-08-01")})
	if !day61.Warn || day61.Blocked {
		t.Errorf("auth day61 should warn: %+v", day61)
	}
}

func TestDeadlineBoundary(t *testing.T) {
	for _, spec := range []Spec{cosignSpec, authSpec} {
		root := setup(t, spec, "2026-06-01", "")
		day90, _ := Check(spec, Config{Root: root, AsOf: at(t, "2026-08-30")})
		if day90.Days != 90 || day90.Blocked {
			t.Errorf("spec %s day90 should not block: %+v", spec.MarkerRelPath, day90)
		}
		day91, _ := Check(spec, Config{Root: root, AsOf: at(t, "2026-08-31")})
		if day91.Days != 91 || !day91.Blocked {
			t.Errorf("spec %s day91 should block: %+v", spec.MarkerRelPath, day91)
		}
	}
}

func TestSignedExceptionPermits(t *testing.T) {
	for _, spec := range []Spec{cosignSpec, authSpec} {
		root := setup(t, spec, "2026-06-01",
			"slip-rationale: vendor outage\napproved-by: ops@example.com 2026-08-30\n")
		o, _ := Check(spec, Config{Root: root, AsOf: at(t, "2026-08-31")})
		if o.Blocked || !o.ExceptionPermitted {
			t.Errorf("spec %s: signed exception should permit: %+v", spec.MarkerRelPath, o)
		}
	}
}

func TestUnsignedExceptionStillBlocks(t *testing.T) {
	for _, spec := range []Spec{cosignSpec, authSpec} {
		root := setup(t, spec, "2026-06-01", "slip-rationale: filed but never signed\n")
		o, _ := Check(spec, Config{Root: root, AsOf: at(t, "2026-08-31")})
		if !o.Blocked || o.ExceptionPermitted {
			t.Errorf("spec %s: unsigned exception should still block: %+v", spec.MarkerRelPath, o)
		}
	}
}

func TestEmptyMarkerErrors(t *testing.T) {
	root := setup(t, cosignSpec, "", "")
	if _, err := Check(cosignSpec, Config{Root: root}); err == nil {
		t.Error("expected error for empty marker")
	}
}

func TestGarbageMarkerErrors(t *testing.T) {
	root := setup(t, authSpec, "not-a-date", "")
	if _, err := Check(authSpec, Config{Root: root}); err == nil {
		t.Error("expected error for garbage marker")
	}
}

func TestFirstLineOnly(t *testing.T) {
	root := setup(t, cosignSpec, "2026-06-01\ntrailing note\n", "")
	o, err := Check(cosignSpec, Config{Root: root, AsOf: at(t, "2026-07-01")})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Date != "2026-06-01" || o.Days != 30 {
		t.Errorf("first-line: date=%q days=%d", o.Date, o.Days)
	}
}

func TestZeroAsOfUsesNow(t *testing.T) {
	root := setup(t, cosignSpec, "2000-01-01", "")
	o, err := Check(cosignSpec, Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Blocked {
		t.Errorf("ancient date should block, got days=%d", o.Days)
	}
}
