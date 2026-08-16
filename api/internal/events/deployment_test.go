// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIsCanonicalStep_KnownNames(t *testing.T) {
	known := []string{"clone-repo", "build-image-api", "sign-image-api", "deliver-artifact-web"}
	for _, n := range known {
		if !IsCanonicalStep(n) {
			t.Fatalf("expected %q to be canonical", n)
		}
	}
}

func TestIsCanonicalStep_UnknownReturnsFalse(t *testing.T) {
	if IsCanonicalStep("custom-step-not-in-allowlist") {
		t.Fatal("expected unknown step to return false")
	}
}

func TestIsCanonicalStep_EmptyReturnsFalse(t *testing.T) {
	if IsCanonicalStep("") {
		t.Fatal("expected empty step to return false")
	}
}

func TestPhaseGroupForStep_AllCanonicalMappedToKnownGroup(t *testing.T) {
	for _, name := range CanonicalStepNames {
		group := PhaseGroupForStep(name)
		switch group {
		case "clone", "install", "build", "sign", "deliver":
			// ok
		default:
			t.Fatalf("canonical step %q mapped to unknown group %q", name, group)
		}
	}
}

func TestPhaseGroupForStep_UnknownReturnsOther(t *testing.T) {
	if got := PhaseGroupForStep("custom"); got != "other" {
		t.Fatalf("expected unknown step to map to other, got %q", got)
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
