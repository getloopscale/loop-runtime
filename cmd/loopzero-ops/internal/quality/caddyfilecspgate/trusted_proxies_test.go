// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package caddyfilecspgate

import (
	"strings"
	"testing"
)

func TestExtractTrustedProxiesBlock_BraceForm(t *testing.T) {
	body, ok := extractTrustedProxiesBlock("servers {\n\ttrusted_proxies static {\n\t\t173.245.48.0/20\n\t}\n}\n")
	if !ok || !strings.Contains(body, "173.245.48.0/20") || strings.Contains(body, "}") {
		t.Fatalf("want the brace body, got %q %v", body, ok)
	}
}

func TestExtractTrustedProxiesBlock_UnbalancedBraceFormIsNotFound(t *testing.T) {
	if _, ok := extractTrustedProxiesBlock("trusted_proxies static {\n\t173.245.48.0/20\n"); ok {
		t.Fatal("an unbalanced block must not be returned")
	}
}

func TestExtractTrustedProxiesBlock_LineContinuationForm(t *testing.T) {
	body, ok := extractTrustedProxiesBlock("trusted_proxies static 173.245.48.0/20 \\\n\t103.21.244.0/22\nlog\n")
	if !ok || !strings.Contains(body, "103.21.244.0/22") || strings.Contains(body, "log") {
		t.Fatalf("want both CIDRs and nothing after, got %q %v", body, ok)
	}
}
