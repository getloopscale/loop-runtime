// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import "testing"

func TestVerdict_String(t *testing.T) {
	cases := map[Verdict]string{PASS: "PASS", SKIP: "SKIP", FAIL: "FAIL"}
	for v, want := range cases {
		if got := v.String(); got != want {
			t.Errorf("Verdict(%d).String() = %q, want %q", int(v), got, want)
		}
	}
}

func TestVerdict_ExitCode(t *testing.T) {
	// PASS/SKIP are success (0); FAIL is 2 (build/exec failure class, CLAUDE.md Rule 4).
	if PASS.ExitCode() != 0 || SKIP.ExitCode() != 0 {
		t.Errorf("PASS/SKIP must exit 0, got %d/%d", PASS.ExitCode(), SKIP.ExitCode())
	}
	if FAIL.ExitCode() != 2 {
		t.Errorf("FAIL must exit 2, got %d", FAIL.ExitCode())
	}
}

func TestRegistry_OrderStable(t *testing.T) {
	// Registry iteration MUST be deterministic (canonical chain order), not map-random.
	r := NewRegistry()
	r.Register(stubSeed{name: "b-second"})
	r.Register(stubSeed{name: "a-first"})
	r.Register(stubSeed{name: "c-third"})
	got := r.Names()
	want := []string{"b-second", "a-first", "c-third"} // insertion order preserved
	if len(got) != len(want) {
		t.Fatalf("Names() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q (insertion order)", i, got[i], want[i])
		}
	}
}

func TestRegistry_DuplicateNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Register of duplicate name must panic (programming error)")
		}
	}()
	r := NewRegistry()
	r.Register(stubSeed{name: "dup"})
	r.Register(stubSeed{name: "dup"})
}

type stubSeed struct{ name string }

func (s stubSeed) Name() string                { return s.name }
func (s stubSeed) Run(Deps) Result             { return Result{Verdict: PASS} }
func (s stubSeed) Verify(Deps) Result          { return Result{Verdict: PASS} }
