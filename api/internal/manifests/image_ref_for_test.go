// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T3.2 (M3 + EC-11) — RenderOpts.ImageRefFor contract tests.

package manifests

import (
	"strings"
	"testing"
)

func TestImageRefFor_PerServiceWins(t *testing.T) {
	opts := RenderOpts{
		ImageRefByName: map[string]string{"api": "registry/api:v1"},
		// Per-service ref takes precedence even when default is set.
		ImageRefDefault: "registry/default:v0",
	}
	ref, err := opts.ImageRefFor("api")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if ref != "registry/api:v1" {
		t.Errorf("expected per-service ref, got %q", ref)
	}
}

func TestImageRefFor_FallsBackToDefault(t *testing.T) {
	opts := RenderOpts{ImageRefDefault: "registry/default:v0"}
	ref, err := opts.ImageRefFor("api")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if ref != "registry/default:v0" {
		t.Errorf("expected default fallback, got %q", ref)
	}
}

func TestImageRefFor_NoRefNoDefault_ReturnsError(t *testing.T) {
	opts := RenderOpts{}
	_, err := opts.ImageRefFor("api")
	if err == nil {
		t.Fatal("expected actionable error, got nil")
	}
	if !strings.Contains(err.Error(), "image ref missing") || !strings.Contains(err.Error(), "api") {
		t.Errorf("expected actionable error mentioning service name, got: %v", err)
	}
}

// EC-11: AllowMissingImage opens the fixture-rendering path.
func TestImageRefFor_AllowMissingImage_FixtureMode(t *testing.T) {
	opts := RenderOpts{AllowMissingImage: true}
	ref, err := opts.ImageRefFor("api")
	if err != nil {
		t.Fatalf("AllowMissingImage must suppress error, got: %v", err)
	}
	if ref != "" {
		t.Errorf("AllowMissingImage path must return empty ref, got %q", ref)
	}
}

// EC-11: empty per-service ref still falls through to default / error, not
// silently honored as a valid empty ref.
func TestImageRefFor_EmptyPerServiceRef_FallsThrough(t *testing.T) {
	opts := RenderOpts{
		ImageRefByName:  map[string]string{"api": ""}, // explicitly empty
		ImageRefDefault: "registry/default:v0",
	}
	ref, err := opts.ImageRefFor("api")
	if err != nil {
		t.Fatalf("empty per-service ref must fall through to default, got err: %v", err)
	}
	if ref != "registry/default:v0" {
		t.Errorf("expected fallback to default when per-service ref is empty, got %q", ref)
	}
}
