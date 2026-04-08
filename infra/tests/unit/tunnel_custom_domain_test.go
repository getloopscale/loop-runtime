// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// pulumiSrcDir returns the absolute path to the Pulumi source directory.
func pulumiSrcDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "pulumi", "src")
}

// TestTunnelCatchAllRoutesToTraefik verifies that the CF Tunnel ingress config
// routes ALL traffic to Traefik (not http_status:404) to support custom domains.
// MEET-2026-04-05-033 Blocker B1: catch-all must send traffic to Traefik.
func TestTunnelCatchAllRoutesToTraefik(t *testing.T) {
	tunnelPath := filepath.Join(pulumiSrcDir(), "providers", "cloudflare", "tunnel.ts")
	data, err := os.ReadFile(tunnelPath)
	if err != nil {
		t.Fatalf("cannot read tunnel.ts: %v", err)
	}
	content := string(data)

	// The catch-all rule (last ingress rule, no hostname) must route to Traefik,
	// NOT return http_status:404. Custom domains CNAME to cname.{baseDomain},
	// which needs the catch-all to forward to Traefik for Host() matching.
	if strings.Contains(content, `service: "http_status:404"`) {
		t.Error("tunnel.ts catch-all routes to http_status:404 — custom domains will fail.\n" +
			"The catch-all ingress rule must route to Traefik (config.traefikService).\n" +
			"See: MEET-2026-04-05-033, docs/plans/custom-domains-design.md §6.2")
	}

	// Verify catch-all exists (required by Cloudflare — last rule has no hostname)
	// The pattern: a service line that references traefikService without a hostname
	// appearing after the wildcard rule.
	if !strings.Contains(content, "Catch-all") {
		t.Error("tunnel.ts missing catch-all comment marker — " +
			"ensure the last ingress rule is a catch-all that routes to Traefik")
	}
}

// TestTunnelHasCnameDnsRecord verifies that the tunnel creates a
// cname.{baseDomain} DNS record for custom domain users.
// MEET-2026-04-05-033 Blocker B1: stable CNAME target for custom domains.
func TestTunnelHasCnameDnsRecord(t *testing.T) {
	tunnelPath := filepath.Join(pulumiSrcDir(), "providers", "cloudflare", "tunnel.ts")
	data, err := os.ReadFile(tunnelPath)
	if err != nil {
		t.Fatalf("cannot read tunnel.ts: %v", err)
	}
	content := string(data)

	// Must create a DNS record for cname.{baseDomain}
	if !strings.Contains(content, "cname.${config.baseDomain}") {
		t.Error("tunnel.ts missing cname.{baseDomain} DNS record.\n" +
			"Custom domain users CNAME their domain to cname.{baseDomain}.\n" +
			"See: MEET-2026-04-05-033, docs/plans/custom-domains-design.md §6.3")
	}

	// The cname record must be a CNAME pointing to the tunnel
	if !strings.Contains(content, `"loopzero-custom-domain-cname-target"`) {
		t.Error("tunnel.ts missing loopzero-custom-domain-cname-target DNS record resource")
	}
}

// TestTunnelSupportsCustomDomainComments verifies the tunnel config documents
// custom domain support for maintainability.
func TestTunnelSupportsCustomDomainComments(t *testing.T) {
	tunnelPath := filepath.Join(pulumiSrcDir(), "providers", "cloudflare", "tunnel.ts")
	data, err := os.ReadFile(tunnelPath)
	if err != nil {
		t.Fatalf("cannot read tunnel.ts: %v", err)
	}
	content := string(data)

	requiredDocs := []string{
		"Custom domains",
		"MEET-2026-04-05-033",
	}

	for _, doc := range requiredDocs {
		if !strings.Contains(content, doc) {
			t.Errorf("tunnel.ts missing documentation reference: %q", doc)
		}
	}
}
