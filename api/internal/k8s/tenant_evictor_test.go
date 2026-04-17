// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import "testing"

func TestHasTenantPrefix(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		tenants  []string
		want     bool
	}{
		{"exact match on tenant ID", "tn-lt-l3-01", []string{"tn-lt-l3-01"}, true},
		{"prefix match on CR embedding tenant", "tn-lt-l3-01-environment", []string{"tn-lt-l3-01"}, true},
		{"prefix match on different tenant doesn't match", "tn-other-5", []string{"tn-lt-l3-01"}, false},
		{"empty tenants never matches", "tn-lt-l3-01", nil, false},
		{"empty name never matches any prefix", "", []string{"tn-lt"}, false},
		{"multi-tenant list — any prefix wins", "tn-other-9-app", []string{"tn-lt", "tn-other"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasTenantPrefix(tc.resource, tc.tenants); got != tc.want {
				t.Fatalf("hasTenantPrefix(%q, %v) = %v, want %v", tc.resource, tc.tenants, got, tc.want)
			}
		})
	}
}

func TestRuntimeNamespaceMatches(t *testing.T) {
	tests := []struct {
		name    string
		ns      string
		tenants []string
		want    bool
	}{
		{"loopzero-<tenant>-<env>", "loopzero-tn-lt-l3-01-production", []string{"tn-lt-l3-01"}, true},
		{"loopzero-<tenant> bare", "loopzero-tn-lt-l3-01", []string{"tn-lt-l3-01"}, true},
		{"missing loopzero- prefix", "tn-lt-l3-01-production", []string{"tn-lt-l3-01"}, false},
		{"matching tenant prefix but non-loopzero ns", "platform-system", []string{"tn-lt-l3-01"}, false},
		{"different tenant", "loopzero-tn-other-production", []string{"tn-lt-l3-01"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeNamespaceMatches(tc.ns, tc.tenants); got != tc.want {
				t.Fatalf("runtimeNamespaceMatches(%q, %v) = %v, want %v", tc.ns, tc.tenants, got, tc.want)
			}
		})
	}
}
