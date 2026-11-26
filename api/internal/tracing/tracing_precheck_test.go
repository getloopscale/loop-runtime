// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tracing

import (
	"strings"
	"testing"
)

// TestPreCheckOTelEndpoint_UnresolvableEndpoint asserts that an unresolvable
// hostname returns a non-nil error (so caller can register no-op exporter
// instead of letting gRPC client retry-loop forever).
// Plan T3.1 RED #1.
func TestPreCheckOTelEndpoint_UnresolvableEndpoint(t *testing.T) {
	// .invalid TLD is RFC 2606 reserved and never resolves
	err := preCheckOTelEndpoint("definitely-does-not-exist.invalid:4317")
	if err == nil {
		t.Fatalf("expected error for unresolvable host, got nil")
	}
	if !strings.Contains(err.Error(), "LookupHost") {
		t.Errorf("expected LookupHost error, got %q", err.Error())
	}
}

// TestPreCheckOTelEndpoint_HappyPath asserts that a resolvable hostname
// (localhost) returns nil error.
// Plan T3.1 RED #2.
func TestPreCheckOTelEndpoint_HappyPath(t *testing.T) {
	if err := preCheckOTelEndpoint("localhost:4317"); err != nil {
		t.Errorf("expected nil for localhost, got %v", err)
	}
}

// TestPreCheckOTelEndpoint_EmptyEndpoint asserts that empty input is an error.
func TestPreCheckOTelEndpoint_EmptyEndpoint(t *testing.T) {
	if err := preCheckOTelEndpoint(""); err == nil {
		t.Errorf("expected error for empty endpoint, got nil")
	}
}

// TestPreCheckOTelEndpoint_StripsSchemeAndPath ensures URL-like input still
// resolves correctly (we strip http:// + path before LookupHost).
func TestPreCheckOTelEndpoint_StripsSchemeAndPath(t *testing.T) {
	if err := preCheckOTelEndpoint("http://localhost:4317/v1/traces"); err != nil {
		t.Errorf("expected nil for URL form, got %v", err)
	}
}
