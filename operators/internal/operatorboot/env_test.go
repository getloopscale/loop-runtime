// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package operatorboot

import (
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestPositiveInt_UnsetReturnsTheDefault(t *testing.T) {
	got, err := PositiveInt(envOf(nil), "N", 5)
	if err != nil || got != 5 {
		t.Fatalf("got (%d, %v), want (5, nil)", got, err)
	}
}

func TestPositiveInt_ReadsAPositiveValue(t *testing.T) {
	got, err := PositiveInt(envOf(map[string]string{"N": "12"}), "N", 5)
	if err != nil || got != 12 {
		t.Fatalf("got (%d, %v), want (12, nil)", got, err)
	}
}

// A malformed or non-positive value is a configuration error the operator
// must refuse to start on, not a value to ignore in silence.
func TestPositiveInt_RefusesMalformedAndNonPositiveValues(t *testing.T) {
	for _, raw := range []string{"five", "0", "-3", "1.5"} {
		if _, err := PositiveInt(envOf(map[string]string{"N": raw}), "N", 5); err == nil {
			t.Errorf("PositiveInt(%q) accepted", raw)
		}
	}
}

func TestPositiveDuration_ReadsAndDefaults(t *testing.T) {
	if got, err := PositiveDuration(envOf(nil), "D", 30*time.Second); err != nil || got != 30*time.Second {
		t.Fatalf("unset: got (%v, %v)", got, err)
	}
	if got, err := PositiveDuration(envOf(map[string]string{"D": "45s"}), "D", 30*time.Second); err != nil || got != 45*time.Second {
		t.Fatalf("45s: got (%v, %v)", got, err)
	}
}

func TestPositiveDuration_RefusesMalformedAndNonPositiveValues(t *testing.T) {
	for _, raw := range []string{"30", "soon", "0s", "-1m"} {
		if _, err := PositiveDuration(envOf(map[string]string{"D": raw}), "D", time.Second); err == nil {
			t.Errorf("PositiveDuration(%q) accepted", raw)
		}
	}
}
