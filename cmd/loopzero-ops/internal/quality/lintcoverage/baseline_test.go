// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func base(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lint-baseline.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const frozenBaseline = `{"lint":{"api":0,"cmd/loopzero-ops":221},"gofmt":{"api":70,"cmd/loopzero-ops":0}}`

func TestEqualToFrozenPasses(t *testing.T) {
	b, err := LoadBaseline(base(t, frozenBaseline))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if v := b.Compare(map[string]int{"api": 0, "cmd/loopzero-ops": 221}, b.Lint, "lint"); len(v.Regressions) != 0 {
		t.Fatalf("equal to frozen passes: %+v", v.Regressions)
	}
}

// The ratchet. A module at 0 that gains ONE finding fails, even with 221 frozen beside it —
// that is what keeps the baseline from turning into permission.
func TestAboveFrozenFailsNamingTheModule(t *testing.T) {
	b, _ := LoadBaseline(base(t, frozenBaseline))
	v := b.Compare(map[string]int{"api": 1, "cmd/loopzero-ops": 221}, b.Lint, "lint")
	if len(v.Regressions) != 1 || v.Regressions[0].Module != "api" {
		t.Fatalf("the regression must NAME the module: %+v", v.Regressions)
	}
	if v.Regressions[0].Was != 0 || v.Regressions[0].Now != 1 {
		t.Fatalf("the message must carry before and after: %+v", v.Regressions[0])
	}
}

// Improving passes — and the gate SAYS it can be refrozen, otherwise the number never comes down.
func TestBelowFrozenPassesAndSignals(t *testing.T) {
	b, _ := LoadBaseline(base(t, frozenBaseline))
	v := b.Compare(map[string]int{"api": 0, "cmd/loopzero-ops": 200}, b.Lint, "lint")
	if len(v.Regressions) != 0 {
		t.Fatalf("improving does not fail: %+v", v.Regressions)
	}
	if len(v.Improvements) != 1 || v.Improvements[0].Module != "cmd/loopzero-ops" {
		t.Fatalf("the improvement must show up so that someone refreezes: %+v", v.Improvements)
	}
}

// A new module enters DECLARED. Without this, a module created today would be born with
// invisible findings — which is exactly the defect this slice fixes.
func TestNewModuleWithoutEntryFails(t *testing.T) {
	b, _ := LoadBaseline(base(t, frozenBaseline))
	v := b.Compare(map[string]int{"api": 0, "cmd/loopzero-ops": 221, "module/new": 5}, b.Lint, "lint")
	if len(v.Undeclared) != 1 || v.Undeclared[0] != "module/new" {
		t.Fatalf("a module with no entry must be reported: %+v", v.Undeclared)
	}
}

func TestMissingBaselineFails(t *testing.T) {
	if _, err := LoadBaseline(filepath.Join(t.TempDir(), "does-not-exist.json")); !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("a missing baseline fails; err=%v", err)
	}
}

func TestUnreadableBaselineFails(t *testing.T) {
	if _, err := LoadBaseline(base(t, "{this is not json")); err == nil {
		t.Fatal("an unreadable baseline cannot be read as empty")
	}
}

// Rewriting twice must yield identical bytes — otherwise the diff is noise and nobody reviews it.
func TestBaselineIsDeterministic(t *testing.T) {
	d := t.TempDir()
	lint := map[string]int{"pkg": 0, "api": 0, "cmd/loopzero-ops": 221}
	fmtc := map[string]int{"api": 70, "pkg": 30}
	p1, p2 := filepath.Join(d, "1.json"), filepath.Join(d, "2.json")
	for _, p := range []string{p1, p2} {
		if err := WriteBaseline(p, lint, fmtc); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	a, _ := os.ReadFile(p1)
	b, _ := os.ReadFile(p2)
	if string(a) != string(b) {
		t.Fatal("rewriting twice must yield identical bytes")
	}
	got, err := LoadBaseline(p1)
	if err != nil || got.Lint["cmd/loopzero-ops"] != 221 || got.Gofmt["api"] != 70 {
		t.Fatalf("round trip: %v %+v", err, got)
	}
}
