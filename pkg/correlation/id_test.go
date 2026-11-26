// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package correlation

import (
	"regexp"
	"testing"
)

var uuidV4Regex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestGenerateID(t *testing.T) {
	t.Run("returns a valid UUID v4", func(t *testing.T) {
		id := GenerateID()
		if !uuidV4Regex.MatchString(id) {
			t.Errorf("expected UUID v4, got %q", id)
		}
	})

	t.Run("returns unique values on each call", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			id := GenerateID()
			if seen[id] {
				t.Fatalf("duplicate ID found: %s", id)
			}
			seen[id] = true
		}
	})
}
