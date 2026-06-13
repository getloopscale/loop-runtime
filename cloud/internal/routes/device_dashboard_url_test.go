// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"strings"
	"testing"
)

// T3.6 of plan 2026-06-11-code-review-remediation v1.2 + ADR D5 pillar-(b)
// test contract. Closes the second of the 3 bug-class proofs the plan
// requires: device.go.DeviceHandler.dashboardURL must read from its
// constructor argument and MUST NOT regress to a hardcoded literal.
//
// Bug shape pinned by this test (taken from the canonical drift the
// /loop-code-review found 2026-06-11): a future refactor "simplifying"
// the constructor by inlining a hardcoded `https://app-dev.loopzero.example`
// (or, worse, the pre-T2.1 mistyped `https://app.dev.loopzero.example`)
// would break self-hosted / GHES / alt-cell operators silently.
//
// Per ADR D5 EC-13: each pillar-(b) test pairs a POSITIVE assertion
// (injected value flows through) with an ANTI-ASSERTION (the literal
// from the original bug evidence does NOT appear). The anti-assertion
// is the regression guard tied to the original bug shape; the positive
// assertion is the behavior contract.

// TestNewDeviceHandler_ReadsConfigSource_DashboardURL — pillar-(b)
// proof: DeviceHandler accepts a dashboardURL argument and stores it
// verbatim. Defends against a refactor that would inline a hardcoded
// fallback (the silent-config-source-loss class of bug).
func TestNewDeviceHandler_ReadsConfigSource_DashboardURL(t *testing.T) {
	const injected = "https://my-custom.dashboard.example.com"
	h := NewDeviceHandler(nil, injected)

	// Positive assertion: the injected config flowed into the struct
	// field. We intentionally read the private field because the
	// contract under test is the CONSTRUCTOR plumbing, not the public
	// handler behavior (which doesn't consume dashboardURL today —
	// future flows will, and the constructor MUST preserve the value
	// so they can).
	if h.dashboardURL != injected {
		t.Errorf("NewDeviceHandler dropped dashboardURL: got %q, want %q",
			h.dashboardURL, injected)
	}

	// Anti-assertion (ADR D5 EC-13): the pre-T2.1 misshaped literal
	// `app.dev.loopzero.example` (dot instead of hyphen — NOT a real DNS
	// name) MUST NOT appear when the operator injected a custom URL.
	// A future bug that re-introduces a hardcoded fallback would
	// regress THIS specific shape; the assertion catches it.
	if strings.Contains(h.dashboardURL, "app.dev.loopzero.example") {
		t.Errorf("dashboardURL regressed to the pre-T2.1 hardcoded literal: %q",
			h.dashboardURL)
	}
	// Defense-in-depth: also reject the canonical default fallback.
	// When the operator injected a custom URL, the canonical default
	// MUST NOT leak through.
	if strings.Contains(h.dashboardURL, "app-dev.loopzero.example") {
		t.Errorf("dashboardURL regressed to the canonical default despite custom injection: %q",
			h.dashboardURL)
	}
}

// TestNewDeviceHandler_AcceptsCanonicalDefaultDashboardURL — companion
// positive case. When the operator deliberately injects the canonical
// `https://app-dev.loopzero.example`, the test asserts the value is stored
// verbatim AND the anti-assertion (no DOT pattern) holds.
func TestNewDeviceHandler_AcceptsCanonicalDefaultDashboardURL(t *testing.T) {
	const canonical = "https://app-dev.loopzero.example"
	h := NewDeviceHandler(nil, canonical)

	if h.dashboardURL != canonical {
		t.Errorf("canonical dashboardURL dropped: got %q, want %q",
			h.dashboardURL, canonical)
	}
	// Anti-assertion: the dot-instead-of-hyphen pre-T2.1 literal MUST
	// NOT appear even when the canonical hyphen-form is the input.
	if strings.Contains(h.dashboardURL, "app.dev.loopzero.example") {
		t.Errorf("canonical dashboardURL contains pre-T2.1 wrong pattern: %q",
			h.dashboardURL)
	}
}
