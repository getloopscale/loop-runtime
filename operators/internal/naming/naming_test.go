package naming

import (
	"strings"
	"testing"
)

func TestNamespaceName_ValidInput(t *testing.T) {
	name, err := NamespaceName("acme", "my-saas", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "acme-my-saas-production" {
		t.Errorf("expected %q, got %q", "acme-my-saas-production", name)
	}
}

func TestNamespaceName_TooLong(t *testing.T) {
	// Create a name that exceeds 63 characters.
	long := strings.Repeat("a", 30)
	_, err := NamespaceName(long, long, "production")
	if err == nil {
		t.Error("expected error for name exceeding 63 characters")
	}
}

func TestBuildNamespaceName_ValidInput(t *testing.T) {
	name, err := BuildNamespaceName("acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "build-acme" {
		t.Errorf("expected %q, got %q", "build-acme", name)
	}
}

func TestPreviewNamespaceName_ValidInput(t *testing.T) {
	name, err := PreviewNamespaceName("acme", "my-saas", 142)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "acme-my-saas-pr-142" {
		t.Errorf("expected %q, got %q", "acme-my-saas-pr-142", name)
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
	// Edge case: single alphanumeric character is valid.
	if err := ValidateKubernetesName("a"); err != nil {
		t.Errorf("single character should be valid: %v", err)
	}
	if err := ValidateKubernetesName("0"); err != nil {
		t.Errorf("single digit should be valid: %v", err)
	}
}
