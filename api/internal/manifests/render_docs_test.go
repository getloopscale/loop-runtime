// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Plan v1.2 T4.1 — typed K8sDoc + RenderDocs + Concat tests.

package manifests

import (
	"strings"
	"testing"
)

func TestRender_ReturnsK8sDocList(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/a/b:c"},
	}
	docs, err := RenderDocs(input)
	if err != nil {
		t.Fatalf("RenderDocs failed: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected at least one K8sDoc, got 0")
	}
	for i, d := range docs {
		if d.Kind == "" {
			t.Errorf("doc %d Kind must be populated, got empty (yaml first line: %q)",
				i, firstLine(d.YAML))
		}
		if d.YAML == "" {
			t.Errorf("doc %d YAML must be populated", i)
		}
	}
}

func TestConcat_JoinsWithSeparator(t *testing.T) {
	docs := []K8sDoc{
		{YAML: "kind: A\nname: a"},
		{YAML: "kind: B\nname: b"},
	}
	got := Concat(docs)
	if !strings.Contains(got, "kind: A") {
		t.Errorf("missing first doc body, got: %q", got)
	}
	if !strings.Contains(got, "kind: B") {
		t.Errorf("missing second doc body, got: %q", got)
	}
	if !strings.Contains(got, "\n---\n") {
		t.Errorf("missing separator, got: %q", got)
	}
}

// EC-10 (v1.1) — Concat strips trailing newlines so separator does not
// produce "kind: A\n\n---\nkind: B".
func TestConcat_NoDoubleSeparator_OnTrailingNewline(t *testing.T) {
	docs := []K8sDoc{
		{YAML: "kind: X\n"},
		{YAML: "kind: Y"},
	}
	got := Concat(docs)
	want := "kind: X\n---\nkind: Y"
	if got != want {
		t.Errorf("EC-10: expected %q, got %q", want, got)
	}
}

func TestK8sDoc_KindNameNamespace_Populated(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/a/b:c"},
	}
	docs, err := RenderDocs(input)
	if err != nil {
		t.Fatalf("RenderDocs failed: %v", err)
	}
	// Find the Deployment doc and assert its metadata fields are populated.
	var found bool
	for _, d := range docs {
		if d.Kind == "Deployment" {
			found = true
			if d.Name == "" {
				t.Errorf("Deployment doc Name should be populated, got empty (yaml head: %q)",
					firstLine(d.YAML))
			}
			if d.Namespace == "" {
				t.Errorf("Deployment doc Namespace should be populated, got empty")
			}
		}
	}
	if !found {
		t.Fatal("expected at least one Deployment doc")
	}
}

func TestConcat_EmptySlice_ReturnsEmpty(t *testing.T) {
	if got := Concat(nil); got != "" {
		t.Errorf("expected empty string for nil slice, got %q", got)
	}
	if got := Concat([]K8sDoc{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
