// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unhomedlogic

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func TestAnAbsentAllowlistExemptsNothing(t *testing.T) {
	md, absent, err := LoadAllowlist(filepath.Join(t.TempDir(), "missing.md"))

	if err != nil || !absent || md != "" {
		t.Fatalf("LoadAllowlist = (%q, %v, %v), want empty, absent, no error", md, absent, err)
	}
}

func TestAnAllowlistThatCannotBeReadIsAnError(t *testing.T) {
	_, _, err := LoadAllowlist(t.TempDir()) // a directory is present and unreadable as a file

	if err == nil {
		t.Fatal("a present, unreadable allowlist must be an error")
	}
}

func TestRatchetFailsOnAnIncrease(t *testing.T) {
	base := Counts{Total: 1, ByDomain: map[string]int{"infra": 1}}
	cur := Counts{Total: 2, ByDomain: map[string]int{"infra": 2}}

	if got := Ratchet(cur, base).Verdict(); got != verdict.Fail {
		t.Fatalf("Verdict() = %v, want FAIL", got)
	}
}

func TestRatchetFailsOnASuspectKeep(t *testing.T) {
	c := Counts{Total: 1, ByDomain: map[string]int{"infra": 1}, SuspectKeeps: []string{"x.sh"}}

	if got := Ratchet(c, c).Verdict(); got != verdict.Fail {
		t.Fatalf("Verdict() = %v, want FAIL", got)
	}
}

func TestRatchetPassesAtOrUnderTheBaseline(t *testing.T) {
	base := Counts{Total: 2, ByDomain: map[string]int{"infra": 2}}
	cur := Counts{Total: 1, ByDomain: map[string]int{"infra": 1}}

	if got := Ratchet(cur, base).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

func TestARunThatJudgesNothingIsAPass(t *testing.T) {
	if got := (Outcome{Current: Counts{SuspectKeeps: []string{"x.sh"}}}).Verdict(); got != verdict.Pass {
		t.Fatalf("a report: Verdict() = %v, want PASS", got)
	}
}

func TestWriteBaselineRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.json")
	want := Counts{Total: 3, ByDomain: map[string]int{"infra": 3}}

	if err := WriteBaseline(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadBaseline(path)
	if err != nil || got.Total != 3 || got.ByDomain["infra"] != 3 {
		t.Fatalf("LoadBaseline = (%+v, %v)", got, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		t.Fatalf("the baseline must end with a newline: %q", b)
	}
}

func TestAMissingBaselineIsAReadError(t *testing.T) {
	_, err := LoadBaseline(filepath.Join(t.TempDir(), "missing.json"))

	var re *BaselineReadError
	if !errors.As(err, &re) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a *BaselineReadError wrapping fs.ErrNotExist", err)
	}
}

func TestAnUndecodableBaselineIsNotAReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadBaseline(path)

	var re *BaselineReadError
	if err == nil || errors.As(err, &re) {
		t.Fatalf("err = %v, want the decoder's error", err)
	}
}
