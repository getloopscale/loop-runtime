// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"strings"
	"testing"
)

func TestGenerateProjectID_Format(t *testing.T) {
	id := GenerateProjectID()
	if !strings.HasPrefix(id, "proj-") {
		t.Errorf("expected prefix 'proj-', got: %s", id)
	}
	// proj- + 8 hex chars = 13 chars
	if len(id) != 13 {
		t.Errorf("expected length 13, got: %d", len(id))
	}
}

func TestGenerateProjectID_Unique(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := GenerateProjectID()
		if ids[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}
