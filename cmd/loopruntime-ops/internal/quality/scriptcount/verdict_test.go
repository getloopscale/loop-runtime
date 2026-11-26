// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scriptcount

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func TestTallyVerdictFailsAboveTheCeiling(t *testing.T) {
	if got := (Tally{Count: 3, Baseline: 2}).Verdict(); got != verdict.Fail {
		t.Fatalf("above: Verdict() = %v, want FAIL", got)
	}
	if got := (Tally{Count: 2, Baseline: 2, OK: true}).Verdict(); got != verdict.Pass {
		t.Fatalf("at the ceiling: Verdict() = %v, want PASS", got)
	}
}

func TestRatchetRefusesToRaiseTheCeiling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, []byte("{\n  \"canonical_sh\": 2\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := Ratchet(path, Tally{Count: 3, Baseline: 2})

	if err != nil || r.Verdict() != verdict.Fail {
		t.Fatalf("err=%v verdict=%v, want a refused ratchet", err, r.Verdict())
	}
	if body := readFile(t, path); body != "{\n  \"canonical_sh\": 2\n}\n" {
		t.Fatalf("a refused ratchet must leave the baseline untouched, got %q", body)
	}
}

func TestRatchetLowersTheCeilingToTheCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")

	r, err := Ratchet(path, Tally{Count: 1, Baseline: 2, OK: true, Improved: true})

	if err != nil || r.Verdict() != verdict.Pass {
		t.Fatalf("err=%v verdict=%v", err, r.Verdict())
	}
	if body := readFile(t, path); body != "{\n  \"canonical_sh\": 1\n}\n" {
		t.Fatalf("baseline = %q, want the count", body)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
