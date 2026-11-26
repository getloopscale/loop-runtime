// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package httpclient

import (
	"crypto/tls"
	"net/http"
	"testing"
)

// TestSkipTLSVerify_DefaultIsSecure enforces secure-by-default per
// COMP-007 / SEC-001 / T2.5. Unsetting OPERATOR_INSECURE_SKIP_TLS must
// produce TLS verification, not skip.
//
// Plan ref: docs/plans/operators-deep-review-remediation-plan.md T2.5.
// ADR ref: .claude/decisions/2026-04-26-T2-5-tls-default.md.
// Invariant: INV-006 (secure_by_default_tls_verification).
func TestSkipTLSVerify_DefaultIsSecure(t *testing.T) {
	t.Setenv("OPERATOR_INSECURE_SKIP_TLS", "")
	if skipTLSVerify() {
		t.Error("default should be secure (verify TLS); got skip=true")
	}
}

func TestSkipTLSVerify_OptInTrue(t *testing.T) {
	cases := []string{"true", "1", "TRUE", "True"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv("OPERATOR_INSECURE_SKIP_TLS", v)
			if !skipTLSVerify() {
				t.Errorf("OPERATOR_INSECURE_SKIP_TLS=%q should opt in to skip-verify", v)
			}
		})
	}
}

func TestSkipTLSVerify_ExplicitFalseStaysSecure(t *testing.T) {
	cases := []string{"false", "0", "FALSE", "no", "off", "anything-else"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv("OPERATOR_INSECURE_SKIP_TLS", v)
			if skipTLSVerify() {
				t.Errorf("OPERATOR_INSECURE_SKIP_TLS=%q should NOT opt in to skip-verify", v)
			}
		})
	}
}

// TestNew_ReturnsClientWithDefaultTransport sanity-checks that New() honors
// the secure default through the actual http.Client / Transport plumbing.
func TestNew_ReturnsClientWithDefaultTransport(t *testing.T) {
	t.Setenv("OPERATOR_INSECURE_SKIP_TLS", "")
	c := New()
	if c == nil {
		t.Fatal("New() returned nil client")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", c.Transport)
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("default httpclient.New() should verify TLS, got InsecureSkipVerify=true")
	}
	// Defensive: tls.Config zero value
	if tr.TLSClientConfig.MinVersion != 0 && tr.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want >= TLS 1.2 or default", tr.TLSClientConfig.MinVersion)
	}
}
