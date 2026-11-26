// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package runtimeslo

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLog(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "go-test.log")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func defaultLimits() Limits { return Limits{PerTestSec: 60, PerSuiteSec: 600} }

// TestCheck_FastTests_Pass — all under limits.
func TestCheck_FastTests_Pass(t *testing.T) {
	log := writeLog(t, "--- PASS: TestFast (1.23s)\nok  github.com/x/pkg  4.5s\n")
	o, err := Check(Config{LogFile: log, Limits: defaultLimits()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass, got %v", o.Violations)
	}
}

// TestCheck_SlowTest_Fail — a test over per-test limit fails.
func TestCheck_SlowTest_Fail(t *testing.T) {
	log := writeLog(t, "--- PASS: TestSlow (75.0s)\n")
	o, err := Check(Config{LogFile: log, Limits: defaultLimits()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (75s > 60s)")
	}
	if len(o.Violations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.Violations)
	}
}

// TestCheck_SlowSuite_Fail — a suite over per-suite limit fails.
func TestCheck_SlowSuite_Fail(t *testing.T) {
	log := writeLog(t, "FAIL  github.com/x/slowpkg  650.0s\n")
	o, err := Check(Config{LogFile: log, Limits: defaultLimits()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (650s > 600s suite)")
	}
}

// TestCheck_FailAndSkipParsed — FAIL and SKIP lines also checked.
func TestCheck_FailAndSkipParsed(t *testing.T) {
	log := writeLog(t, "--- FAIL: TestF (61.0s)\n--- SKIP: TestS (0.0s)\n")
	o, err := Check(Config{LogFile: log, Limits: defaultLimits()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (TestF 61s > 60s)")
	}
}

// TestCheck_MissingLog_Skip — no log file → skip (PASS).
func TestCheck_MissingLog_Skip(t *testing.T) {
	o, err := Check(Config{LogFile: "/nonexistent.log", Limits: defaultLimits()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass || !o.Skipped {
		t.Errorf("a missing log is a skip, not a pass; got pass=%v skipped=%v", o.Pass, o.Skipped)
	}
}

// TestCheck_BoundaryExact_Pass — exactly at limit is NOT over (uses >).
func TestCheck_BoundaryExact_Pass(t *testing.T) {
	log := writeLog(t, "--- PASS: TestEdge (60.0s)\nok  pkg  600.0s\n")
	o, err := Check(Config{LogFile: log, Limits: defaultLimits()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass at exact limits, got %v", o.Violations)
	}
}

// TestParseTestLine — extracts name + duration.
func TestParseTestLine(t *testing.T) {
	name, dur, ok := parseTestLine("    --- PASS: TestX/sub (12.34s)")
	if !ok || name != "TestX/sub" || dur != 12.34 {
		t.Errorf("got name=%q dur=%v ok=%v", name, dur, ok)
	}
	if _, _, ok := parseTestLine("not a test line"); ok {
		t.Error("expected no match on non-test line")
	}
}

// TestParseSuiteLine — extracts suite + duration.
func TestParseSuiteLine(t *testing.T) {
	suite, dur, ok := parseSuiteLine("ok  github.com/getloopscale/loop-runtime/pkg/x  3.21s")
	if !ok || suite != "github.com/getloopscale/loop-runtime/pkg/x" || dur != 3.21 {
		t.Errorf("got suite=%q dur=%v ok=%v", suite, dur, ok)
	}
	// "ok ... (cached)" has no duration → no match
	if _, _, ok := parseSuiteLine("ok  pkg  (cached)"); ok {
		t.Error("expected no match on cached line")
	}
}
