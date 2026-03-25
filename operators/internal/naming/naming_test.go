// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package naming

import (
	"strings"
	"testing"
)

// These tests verify that the naming package correctly delegates to pkg/naming.
// The full naming logic is tested in pkg/naming/naming_test.go; these tests
// confirm the delegation wiring is correct.

func TestEnvironmentNamespace_ValidInput(t *testing.T) {
	name, err := EnvironmentNamespace("tn-loopzero", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "loopzero-tn-loopzero-production" {
		t.Errorf("expected %q, got %q", "loopzero-tn-loopzero-production", name)
	}
}

func TestEnvironmentNamespace_TooLong(t *testing.T) {
	// "loopzero-" prefix (5) + name (30) + "-" (1) + env (30) = 66 chars — exceeds 63
	long := strings.Repeat("a", 30)
	_, err := EnvironmentNamespace(long, long)
	if err == nil {
		t.Error("expected error for name exceeding 63 characters")
	}
}

func TestBuildNamespace_ValidInput(t *testing.T) {
	name, err := BuildNamespace("acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "build-acme" {
		t.Errorf("expected %q, got %q", "build-acme", name)
	}
}

func TestPreviewNamespace_ValidInput(t *testing.T) {
	name, err := PreviewNamespace("acme", "my-saas", 142)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "acme-my-saas-pr-142" {
		t.Errorf("expected %q, got %q", "acme-my-saas-pr-142", name)
	}
}

func TestTenantNamespace_ValidInput(t *testing.T) {
	name, err := TenantNamespace("tn-loopzero")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "loopzero-tn-loopzero" {
		t.Errorf("expected %q, got %q", "loopzero-tn-loopzero", name)
	}
}

func TestValidateKubernetesName_Valid(t *testing.T) {
	validNames := []string{
		"a",
		"abc",
		"a-b-c",
		"my-tenant-123",
		"build-acme",
		strings.Repeat("a", 63),
	}

	for _, name := range validNames {
		if err := ValidateKubernetesName(name); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", name, err)
		}
	}
}

func TestValidateKubernetesName_Invalid(t *testing.T) {
	tests := []struct {
		name string
		desc string
	}{
		{"", "empty name"},
		{"-abc", "starts with hyphen"},
		{"abc-", "ends with hyphen"},
		{"ABC", "uppercase letters"},
		{"my_tenant", "underscore"},
		{"my.tenant", "dot"},
		{strings.Repeat("a", 64), "exceeds 63 chars"},
		{"has space", "contains space"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			if err := ValidateKubernetesName(tc.name); err == nil {
				t.Errorf("expected %q (%s) to be invalid", tc.name, tc.desc)
			}
		})
	}
}

func TestValidateKubernetesName_SingleCharIsValid(t *testing.T) {
	if err := ValidateKubernetesName("a"); err != nil {
		t.Errorf("single character should be valid: %v", err)
	}
	if err := ValidateKubernetesName("0"); err != nil {
		t.Errorf("single digit should be valid: %v", err)
	}
}

func TestMaxKubernetesNameLength_Matches(t *testing.T) {
	if MaxKubernetesNameLength != 63 {
		t.Errorf("expected MaxKubernetesNameLength to be 63, got %d", MaxKubernetesNameLength)
	}
}
