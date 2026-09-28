// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPhaseGroupForStepClassifiesTheNamesTheBuilderEmits(t *testing.T) {
	cases := map[string]string{
		// single-language projects: no suffix
		"clone-repo": "clone", "validate-workspace": "clone",
		"restore-cache": "install", "install-deps": "install", "security-scan": "install",
		"generate-dockerfile": "build", "build-apps": "build", "save-cache": "build",
		// multi-language projects: a language suffix
		"install-deps-python": "install", "build-apps-node": "build", "validate-workspace-python": "clone",
		// per-app steps, for any app name
		"build-image-checkout": "build", "sign-image-checkout": "sign",
		"sbom-checkout": "sign", "attest-image-checkout": "sign",
		"deliver-artifact-docs": "deliver",
	}
	for step, want := range cases {
		if got := PhaseGroupForStep(step); got != want {
			t.Errorf("PhaseGroupForStep(%q) = %q, want %q", step, got, want)
		}
	}
}

func TestPhaseGroupForStepDoesNotMatchAcrossAFamilyBoundary(t *testing.T) {
	for _, step := range []string{"custom", "", "build", "clone-repository", "sbomish", "install"} {
		if got := PhaseGroupForStep(step); got != "other" {
			t.Errorf("PhaseGroupForStep(%q) = %q, want other", step, got)
		}
	}
}

func TestStepMetricLabelIsTheBoundedStepFamily(t *testing.T) {
	cases := map[string]string{
		"build-image-checkout": "build-image",
		"install-deps-python":  "install-deps",
		"install-deps":         "install-deps",
		"clone-repo":           "clone-repo",
		"custom-step":          "other",
	}
	for step, want := range cases {
		if got := StepMetricLabel(step); got != want {
			t.Errorf("StepMetricLabel(%q) = %q, want %q", step, got, want)
		}
	}
}

func TestBuildStepStartedPayload_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := BuildStepStartedPayload{
		StepName:   "clone-repo",
		StartedAt:  now,
		PhaseGroup: "clone",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundtrip BuildStepStartedPayload
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundtrip != original {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", roundtrip, original)
	}
}

func TestBuildStepFailedPayload_IncludesSuggestionField(t *testing.T) {
	p := BuildStepFailedPayload{
		StepName:     "build-image-api",
		ErrorClass:   "OOM",
		ErrorMessage: "Container OOMKilled",
		Suggestion:   "Increase plan resources",
	}
	data, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if _, ok := m["suggestion"]; !ok {
		t.Fatal("suggestion field missing from JSON output")
	}
}
