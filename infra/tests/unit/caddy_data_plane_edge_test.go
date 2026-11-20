// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"regexp"
	"strings"
	"testing"
)

// The public edge serves the data plane at app-dev.getloopscale.com/v1/{memory,rag,lens}/*.
// Until #470 it forwarded those paths to a docker-compose Traefik on :8092 that nothing
// versioned any more, which authorised them against a second loopzero-cloud with its own
// user database. The edge now asks the core cluster's loopzero-cloud (localhost:3002)
// through forward_auth, and proxies to the services' NodePorts on the core cluster.

// dataPlaneRoute is one data-plane prefix on the edge.
type dataPlaneRoute struct {
	prefix   string // /v1/memory/
	authPath string // loopzero-cloud's forward-auth endpoint for it
	upstream string // the service's host port on the core cluster
}

var dataPlaneRoutes = []dataPlaneRoute{
	{"/v1/memory/", "/internal/authz/forward", "localhost:18081"},
	{"/v1/rag/", "/internal/authz/forward-rag", "localhost:18082"},
	{"/v1/lens/", "/internal/authz/forward-lens", "localhost:18084"},
}

// caddyHandle returns the body of the `handle <matcher> {` block inside body, braces
// balanced.
func caddyHandle(t *testing.T, body, matcher string) string {
	t.Helper()
	open := "handle " + matcher + " {"
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("no %q in the block", open)
	}
	depth := 0
	for i := start + len(open) - 1; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[start+len(open) : i]
			}
		}
	}
	t.Fatalf("%q is not closed", open)
	return ""
}

func TestDataPlaneEdgeAuthorisesThroughTheCoreCloudBeforeProxying(t *testing.T) {
	app := caddyBlock(t, caddyfileText(t), "app-dev.getloopscale.com")
	for _, r := range dataPlaneRoutes {
		t.Run(r.prefix, func(t *testing.T) {
			body := caddyHandle(t, app, r.prefix+"*")
			// Caddy's default order runs forward_auth before request_header, which would
			// strip the identity forward-auth just copied; only `route` keeps written order.
			if !strings.Contains(body, "route {") {
				t.Fatal("not a route block — Caddy would reorder its directives")
			}
			order := []string{
				"request_header -X-LoopZero-User-Id",
				"forward_auth localhost:3002",
				"uri " + r.authPath + "\n",
				"copy_headers Authorization X-LoopZero-User-Id",
				"uri path_regexp ^" + r.prefix + " /v1/",
				"reverse_proxy " + r.upstream,
			}
			last := -1
			for _, d := range order {
				i := strings.Index(body, d)
				if i < 0 {
					t.Fatalf("missing %q", d)
				}
				if i < last {
					t.Fatalf("%q runs out of order %v", d, order)
				}
				last = i
			}
		})
	}
}

func TestEdgeNoLongerUsesTheComposeTraefik(t *testing.T) {
	if regexp.MustCompile(`\b8092\b`).MatchString(caddyfileText(t)) {
		t.Error("the Caddyfile still names :8092, the compose Traefik #470 retired from the edge")
	}
}

// loopzero-cloud's /internal/* routes are its forward-auth and MCP resolve endpoints: they
// ARE the authorisation, so they sit outside every auth middleware. Reached from the
// internet, /internal/authz/forward mints a per-tenant data-plane key for any valid
// loopzero_live_* key and hands it back in a response header.
func TestPublicEdgeRefusesTheCloudInternalRoutes(t *testing.T) {
	app := caddyBlock(t, caddyfileText(t), "app-dev.getloopscale.com")
	body := caddyHandle(t, app, "/internal/*")
	if !regexp.MustCompile(`(?m)^\s*respond 404\s*$`).MatchString(body) {
		t.Errorf("handle /internal/* does not answer 404: %q", body)
	}
	if strings.Contains(body, "reverse_proxy") {
		t.Error("handle /internal/* proxies somewhere")
	}
}
