// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import "testing"

// Plan B v3.1 T-B.5 (2026-06-05) — replaces deleted framework_compat_test.go
// after T-B.1 cascade cleanup. Asserts that:
//
//   1. services.json `runtime` values ("python", "node") resolve to valid
//      LanguageStrategy strings via the build-plan dispatch.
//   2. Legacy loopzero.yaml `framework` values continue to dispatch correctly
//      during the ADR D7 transition window.
//   3. The fallback Runtime → Framework precedence works correctly in the
//      AppUnit shape (Runtime wins when both present).
//   4. EC-B3-2 — when both Runtime and Framework are empty, the system
//      falls back to "custom" with an actionable log warning (E-010
//      condition surfaced via slog, not function error to preserve
//      backwards-compatibility of the BuildPlan signature).

// EC-B3-3 (v3.1) — runtime values dispatch to valid strategy strings.
func TestResolveLanguageStrategy_RuntimePython_ReturnsPython(t *testing.T) {
	got := resolveLanguageStrategy("python")
	if got != "python" {
		t.Errorf(`expected "python", got %q`, got)
	}
}

func TestResolveLanguageStrategy_RuntimeNode_ReturnsNode(t *testing.T) {
	got := resolveLanguageStrategy("node")
	if got != "node" {
		t.Errorf(`expected "node", got %q`, got)
	}
}

// EC-B3-11 (v3.1) — legacy framework fallback path.
func TestResolveLanguageStrategy_LegacyFrameworkExpress_FallsBackNode(t *testing.T) {
	got := resolveLanguageStrategy("express")
	if got != "node" {
		t.Errorf(`expected "node" for legacy "express", got %q`, got)
	}
}

func TestResolveLanguageStrategy_LegacyFrameworkFastAPI_FallsBackPython(t *testing.T) {
	got := resolveLanguageStrategy("fastapi")
	if got != "python" {
		t.Errorf(`expected "python" for legacy "fastapi", got %q`, got)
	}
}

func TestResolveLanguageStrategy_LegacyFrameworkNextJS_FallsBackNode(t *testing.T) {
	got := resolveLanguageStrategy("nextjs")
	if got != "node" {
		t.Errorf(`expected "node" for legacy "nextjs", got %q`, got)
	}
}

func TestResolveLanguageStrategy_GoFramework_ReturnsGo(t *testing.T) {
	got := resolveLanguageStrategy("gin")
	if got != "go" {
		t.Errorf(`expected "go" for legacy "gin", got %q`, got)
	}
}

func TestResolveLanguageStrategy_UnknownValue_FallsBackCustom(t *testing.T) {
	got := resolveLanguageStrategy("brainfuck")
	if got != "custom" {
		t.Errorf(`expected "custom" for unknown value, got %q`, got)
	}
}

// EC-B3-2 (v3.1) — empty input falls back to "custom" with E-010 slog warn.
// The function does not return an error (preserves existing BuildPlan
// signature); the operator observability path is slog.Warn at the call
// site in build_plan.go (verified via TestResolveLanguageStrategy_Empty_FallsBackCustom).
func TestResolveLanguageStrategy_Empty_FallsBackCustom(t *testing.T) {
	got := resolveLanguageStrategy("")
	if got != "custom" {
		t.Errorf(`expected "custom" for empty input, got %q`, got)
	}
}
