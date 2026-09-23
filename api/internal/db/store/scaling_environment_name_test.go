// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import "testing"

// The environment becomes part of a Kubernetes namespace, so a scaling config
// may only be saved for a name pkg/naming accepts as an RFC 1123 label. A
// leading or trailing hyphen is exactly the case the hand-rolled rune loop let
// through: a config saved for an environment that can never exist.
func TestScalingConfigValidate_EnvironmentMustBeANamespaceLabel(t *testing.T) {
	for _, env := range []string{"-production", "production-", "Production", "prod_1", ""} {
		c := ScalingConfig{MinReplicas: 1, MaxReplicas: 2, Environment: env}
		if err := c.Validate(); err == nil {
			t.Errorf("environment %q must be rejected", env)
		}
	}
}

func TestScalingConfigValidate_AcceptsAValidEnvironment(t *testing.T) {
	c := ScalingConfig{MinReplicas: 1, MaxReplicas: 2, Environment: "pr-42"}
	if err := c.Validate(); err != nil {
		t.Fatalf("pr-42 is a valid label: %v", err)
	}
}
