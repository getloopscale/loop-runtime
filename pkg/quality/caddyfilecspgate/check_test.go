// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package caddyfilecspgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeCaddyfile writes the given content into a temp dir's
// infra/local/Caddyfile and returns the dir's path.
func makeCaddyfile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	caddyfileDir := filepath.Join(dir, "infra", "local")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyfileDir, "Caddyfile"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

const canonicalAppDevBlock = `app-dev.loopzero.example {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
  }

  @ws {
    header Connection *Upgrade*
    header Upgrade websocket
  }
  reverse_proxy @ws localhost:5173
}
`

func TestCheck_SkippedWhenCaddyfileAbsent(t *testing.T) {
	dir := t.TempDir() // empty dir, no infra/local/Caddyfile
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("want Pass=true on missing Caddyfile, got Pass=%v", out.Pass)
	}
	if !out.Skipped {
		t.Fatalf("want Skipped=true on missing Caddyfile, got Skipped=%v", out.Skipped)
	}
}

func TestCheck_CanonicalAppDevBlockPasses(t *testing.T) {
	dir := makeCaddyfile(t, canonicalAppDevBlock)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("want Pass=true on canonical block, got Pass=%v Violations=%v",
			out.Pass, out.Violations)
	}
	if out.Skipped {
		t.Fatalf("want Skipped=false on canonical block, got Skipped=%v", out.Skipped)
	}
}

func TestCheck_RegressionDetected_CSPPresent(t *testing.T) {
	regressed := strings.Replace(canonicalAppDevBlock,
		`Strict-Transport-Security "max-age=31536000; includeSubDomains"`,
		`Strict-Transport-Security "max-age=31536000; includeSubDomains"
    Content-Security-Policy "default-src 'self'"`, 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when CSP is present (ADR D1 regression), got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_no_csp_in_app_dev_block") {
		t.Fatalf("want CSP-presence violation, got: %v", out.Violations)
	}
}

func TestCheck_RegressionDetected_ViteHandleBlocks(t *testing.T) {
	cases := []struct {
		name      string
		injection string
		marker    string
	}{
		{"handle /src/*", "handle /src/* {\n    respond 404\n  }\n", "`handle /src/*` STILL present"},
		{"handle /@vite/*", "handle /@vite/* {\n    respond 404\n  }\n", "`handle /@vite/*` STILL present"},
		{"handle /node_modules/.vite/*", "handle /node_modules/.vite/* {\n    respond 404\n  }\n", "`handle /node_modules/.vite/*` STILL present"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			regressed := strings.Replace(canonicalAppDevBlock,
				"@ws {", tc.injection+"\n  @ws {", 1)
			dir := makeCaddyfile(t, regressed)
			out, err := Check(Config{RepoRoot: dir})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Pass {
				t.Fatalf("want Pass=false when %s is present (ADR D2 regression), got Pass=true",
					tc.name)
			}
			if !containsViolation(out.Violations, tc.marker) {
				t.Fatalf("want %s violation, got: %v", tc.name, out.Violations)
			}
		})
	}
}

func TestCheck_RegressionDetected_MissingPreservedHeaders(t *testing.T) {
	cases := []struct {
		name      string
		stripFrom string
		marker    string
	}{
		{"X-Frame-Options", `X-Frame-Options "DENY"`, "X-Frame-Options DENY MISSING"},
		{"X-Content-Type-Options", `X-Content-Type-Options "nosniff"`, "X-Content-Type-Options nosniff MISSING"},
		{"Referrer-Policy", `Referrer-Policy "strict-origin-when-cross-origin"`, "Referrer-Policy MISSING"},
		{"HSTS", `Strict-Transport-Security "max-age=31536000; includeSubDomains"`, "HSTS max-age=31536000 MISSING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			regressed := strings.Replace(canonicalAppDevBlock, tc.stripFrom, "", 1)
			dir := makeCaddyfile(t, regressed)
			out, err := Check(Config{RepoRoot: dir})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Pass {
				t.Fatalf("want Pass=false when %s is missing (ADR D3 regression), got Pass=true",
					tc.name)
			}
			if !containsViolation(out.Violations, tc.marker) {
				t.Fatalf("want %s violation, got: %v", tc.name, out.Violations)
			}
		})
	}
}

func TestCheck_RegressionDetected_MissingReverseProxy(t *testing.T) {
	regressed := strings.Replace(canonicalAppDevBlock,
		"reverse_proxy @ws localhost:5173", "", 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when reverse_proxy is removed, got Pass=true")
	}
}

func TestCheck_BlockNotFound(t *testing.T) {
	dir := makeCaddyfile(t, `# no app-dev block here
cloud-dev.loopzero.example {
  reverse_proxy localhost:13002
}
`)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when app-dev block is missing, got Pass=true")
	}
}

func TestCheck_SiblingBlockCSPDoesNotFalseFire(t *testing.T) {
	// A CSP directive in a SIBLING block (cloud-dev) must not trigger the
	// app-dev assertion. This is the canonical block-bound contract.
	twoBlocks := canonicalAppDevBlock + `
cloud-dev.loopzero.example {
  header {
    Content-Security-Policy "default-src 'self'"
  }
  reverse_proxy localhost:13002
}
`
	dir := makeCaddyfile(t, twoBlocks)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("want Pass=true when CSP is only in sibling block, got Pass=false Violations=%v",
			out.Violations)
	}
}

func containsViolation(violations []string, substr string) bool {
	for _, v := range violations {
		if strings.Contains(v, substr) {
			return true
		}
	}
	return false
}
