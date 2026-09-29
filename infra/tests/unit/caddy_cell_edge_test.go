// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The dev-public edge must send a public app host to the cell that runs it. The
// wildcard blocks used to forward every host to cell-0's ingress, so an app placed on
// cell-1 answered 404 at its public URL while it served fine on its own cell (#447).
// The edge now asks the engine which cell serves the host and proxies there.

func caddyfileText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "local", "Caddyfile"))
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// caddyBlock returns the body of the block opened by `header {` at column 0.
func caddyBlock(t *testing.T, text, header string) string {
	t.Helper()
	start := strings.Index(text, "\n"+header+" {")
	if start < 0 {
		t.Fatalf("no block %q in the Caddyfile", header)
	}
	body := text[start+len(header)+3:]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("block %q is not closed", header)
	}
	return body[:end]
}

func TestAppWildcardsRouteThroughTheCellLookup(t *testing.T) {
	text := caddyfileText(t)
	for _, site := range []string{"*.prod-dev.loopzero.example", "*.preview-dev.loopzero.example"} {
		body := caddyBlock(t, text, site)
		if !strings.Contains(body, "import cell_edge") {
			t.Errorf("%s does not import the cell_edge lookup", site)
		}
		if strings.Contains(body, "reverse_proxy https://127.0.0.1:8443") {
			t.Errorf("%s still forwards every host to cell-0's ingress", site)
		}
	}
}

func TestCellEdgeAsksTheEngineAndMapsEveryCell(t *testing.T) {
	body := caddyBlock(t, caddyfileText(t), "(cell_edge)")
	for _, want := range []string{
		"forward_auth localhost:13001",
		"uri /internal/edge/route",
		"copy_headers X-LoopZero-Cell",
		"{$LOOPZERO_EDGE_ROUTE_TOKEN}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("cell_edge lacks %q", want)
		}
	}
	for cell, port := range map[string]string{"cell-0": "8443", "cell-1": "8444"} {
		if !regexp.MustCompile(`(?m)^\s*` + cell + `\s+127\.0\.0\.1:` + port + `\s*$`).MatchString(body) {
			t.Errorf("cell_edge does not map %s to 127.0.0.1:%s", cell, port)
		}
	}
	// A client-supplied X-LoopZero-Cell must never choose the upstream.
	if !strings.Contains(body, "request_header -X-LoopZero-Cell") {
		t.Error("cell_edge does not strip a client-supplied X-LoopZero-Cell before the lookup")
	}
}
