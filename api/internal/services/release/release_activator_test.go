// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"testing"
)

func TestDeriveDeployID_NormalReleaseID(t *testing.T) {
	got := deriveDeployID("rel-8f3a2b1c4d5e6f78")
	if got != "d-8f3a2b1c" {
		t.Errorf("deriveDeployID = %q, want d-8f3a2b1c", got)
	}
}

func TestDeriveDeployID_ShortReleaseID(t *testing.T) {
	got := deriveDeployID("rel-abc")
	if got != "d-abc" {
		t.Errorf("deriveDeployID = %q, want d-abc", got)
	}
}

func TestDeriveDeployID_ExactLength(t *testing.T) {
	got := deriveDeployID("rel-12345678")
	if got != "d-12345678" {
		t.Errorf("deriveDeployID = %q, want d-12345678", got)
	}
}

func TestDeriveDeployID_NoPrefix(t *testing.T) {
	got := deriveDeployID("abcdef1234567890")
	if got != "d-abcdef12" {
		t.Errorf("deriveDeployID = %q, want d-abcdef12", got)
	}
}
