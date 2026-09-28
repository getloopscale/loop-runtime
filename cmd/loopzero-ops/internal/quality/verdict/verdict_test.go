// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package verdict

import "testing"

func TestZeroVerdictIsNotAPass(t *testing.T) {
	var v Verdict

	if v == Pass || v.String() != "NO VERDICT" {
		t.Fatalf("zero verdict = %v (%q); an undecided outcome must not read as a pass", int(v), v)
	}
}

func TestStringNamesEveryVerdict(t *testing.T) {
	want := map[Verdict]string{Pass: "PASS", Fail: "FAIL", Skip: "SKIP", NotVerified: "NOT VERIFIED"}

	for v, s := range want {
		if got := v.String(); got != s {
			t.Errorf("%d.String() = %q, want %q", int(v), got, s)
		}
	}
}

func TestOfFailsOnAnyViolation(t *testing.T) {
	if got := Of([]string{"x"}); got != Fail {
		t.Fatalf("Of(one violation) = %v, want FAIL", got)
	}
}

func TestOfPassesWithNoViolations(t *testing.T) {
	if got := Of[string](nil); got != Pass {
		t.Fatalf("Of(nil) = %v, want PASS", got)
	}
}

func TestFromPassMapsTheBoolean(t *testing.T) {
	if FromPass(true) != Pass || FromPass(false) != Fail {
		t.Fatalf("FromPass(true) = %v, FromPass(false) = %v", FromPass(true), FromPass(false))
	}
}
