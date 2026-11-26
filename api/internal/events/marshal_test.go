// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package events

import (
	"encoding/json"
	"testing"
)

// These 5 cases are the SafeJSONMarshal coverage RELOCATED (not dropped — MA-4)
// from the former api/internal/services/shared, where they were scattered across
// three unrelated test files (release_activator_test.go, coverage_boost2_test.go,
// build_orchestrator_extra_test.go). Consolidated here beside the code they
// exercise. The name-set — ValidInput / UnmarshalableInput / NilInput / MapInput /
// MapWithNestedValues — is the MA-4 parity contract; UnmarshalableInput is the
// load-bearing NEGATIVE case that proves the `{}` fail-safe branch.

func TestSafeJSONMarshal_ValidInput(t *testing.T) {
	data := SafeJSONMarshal(map[string]string{"key": "value"})
	if data == nil {
		t.Fatal("expected non-nil result")
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("expected key=value, got %v", result)
	}
}

func TestSafeJSONMarshal_UnmarshalableInput(t *testing.T) {
	// Channels cannot be marshaled to JSON — the fail-safe branch must return
	// the `{}` fallback so an SSE event still emits (never a panic / broken frame).
	data := SafeJSONMarshal(make(chan int))
	if data == nil {
		t.Fatal("expected non-nil fallback result")
	}
	if string(data) != "{}" {
		t.Errorf("expected empty JSON object fallback, got %q", string(data))
	}
}

func TestSafeJSONMarshal_NilInput(t *testing.T) {
	data := SafeJSONMarshal(nil)
	if data == nil {
		t.Error("expected non-nil result for nil input")
	}
}

func TestSafeJSONMarshal_MapInput(t *testing.T) {
	data := SafeJSONMarshal(map[string]string{"key": "value"})
	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("expected 'value', got %q", parsed["key"])
	}
}

func TestSafeJSONMarshal_MapWithNestedValues(t *testing.T) {
	data := SafeJSONMarshal(map[string]interface{}{
		"deploy_id": "d-abc123",
		"status":    "success",
		"duration":  45,
	})
	if data == nil {
		t.Fatal("expected non-nil result")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["deploy_id"] != "d-abc123" {
		t.Errorf("expected 'd-abc123', got %v", m["deploy_id"])
	}
}
