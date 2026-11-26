// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package validation

import (
	"strings"
	"testing"
)

// One rule for an environment name, wherever it is accepted: it becomes a
// segment of the Kubernetes namespace loopruntime-<tenant>-<project>-<env>, so it is
// a DNS-1123 label, and it starts with a letter.
func TestValidateEnvironmentName(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"production", true},
		{"a", true},
		{"preview-pr-42", true},
		{"j-rollout-a", true},
		{strings.Repeat("a", 63), true},
		{strings.Repeat("a", 64), false},
		{"", false},
		{"staging-", false}, // trailing hyphen: not a DNS-1123 label
		{"1env", false},     // leading digit
		{"-env", false},
		{"Prod", false},
		{"env_1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateEnvironmentName(c.name); (err == nil) != c.valid {
				t.Fatalf("ValidateEnvironmentName(%q) error=%v, want valid=%v", c.name, err, c.valid)
			}
		})
	}
}

func TestIsRFC1123Name(t *testing.T) {
	for name, valid := range map[string]bool{"my-app": true, "1app": true, "app-": false, "-app": false, "App": false, "": false} {
		if IsRFC1123Name(name) != valid {
			t.Errorf("IsRFC1123Name(%q) = %v, want %v", name, !valid, valid)
		}
	}
}
