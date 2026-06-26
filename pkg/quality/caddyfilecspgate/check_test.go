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

// canonicalAppDevBlock is the v2 (file_server prod-bundle) canonical shape per
// plan caddyfile-app-dev-prod-bundle-file-server-canonical ADR D1+D2.
const canonicalAppDevBlock = `app-dev.loopzero.example {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'; script-src 'self'"
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
  }

  root * {env.DASHBOARD_DIST}
  encode gzip
  file_server
  try_files {path} /index.html
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

// (v2 supersedes the v1 TestCheck_RegressionDetected_CSPPresent — CSP is now
// REQUIRED in the app-dev block per ADR D2 of plan
// caddyfile-app-dev-prod-bundle-file-server-canonical. The v2 inverse test
// `TestCheck_RegressionDetected_CSPMissing_v2` asserts the new direction.)

func TestCheck_RegressionDetected_ViteHandleBlocks(t *testing.T) {
	// v2: legacy v1 handle-blocks assertion preserved — defense-in-depth against
	// someone re-introducing the broken prior-plan topology. Inject the handle
	// block AFTER the file_server directive (close to where the prior plan put it).
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
				"file_server", "file_server\n  "+tc.injection, 1)
			dir := makeCaddyfile(t, regressed)
			out, err := Check(Config{RepoRoot: dir})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Pass {
				t.Fatalf("want Pass=false when %s is present (legacy defense-in-depth), got Pass=true",
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

func TestCheck_RegressionDetected_ViteDevReverseProxyPresent(t *testing.T) {
	// v2: Red Team #66 regression-protector — if reverse_proxy @ws localhost:5173
	// is RE-INTRODUCED (operator reverts to Vite dev), the gate MUST fail.
	regressed := strings.Replace(canonicalAppDevBlock,
		"file_server", "reverse_proxy @ws localhost:5173", 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when Vite-dev reverse_proxy is re-introduced (Red Team #66), got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_no_vite_dev_reverse_proxy") {
		t.Fatalf("want Vite-dev reverse_proxy violation, got: %v", out.Violations)
	}
}

func TestCheck_RegressionDetected_FileServerMissing(t *testing.T) {
	regressed := strings.Replace(canonicalAppDevBlock,
		"file_server", "", 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when file_server is missing, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_file_server_in_app_dev_block") {
		t.Fatalf("want file_server violation, got: %v", out.Violations)
	}
}

func TestCheck_RegressionDetected_CSPMissing_v2(t *testing.T) {
	// v2: with file_server serving prod-bundle, CSP MUST be present (ADR D2
	// of plan caddyfile-app-dev-prod-bundle-file-server-canonical).
	regressed := strings.Replace(canonicalAppDevBlock,
		"Content-Security-Policy \"default-src 'self'; script-src 'self'\"", "", 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when CSP is missing in v2 shape, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_csp_restored_in_app_dev_block") {
		t.Fatalf("want CSP-missing violation, got: %v", out.Violations)
	}
}

func TestCheck_RegressionDetected_TryFilesMissing(t *testing.T) {
	regressed := strings.Replace(canonicalAppDevBlock,
		"try_files {path} /index.html", "", 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when try_files SPA fallback is missing, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_spa_fallback_try_files") {
		t.Fatalf("want try_files violation, got: %v", out.Violations)
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

func TestCheck_SiblingBlockNotEvaluated(t *testing.T) {
	// Block-bound contract: a Vite-dev reverse_proxy in a SIBLING block
	// (cloud-dev) must not trigger the app-dev `assert_no_vite_dev_reverse_proxy`
	// assertion. The canonical block-bound extractor isolates the app-dev block.
	twoBlocks := canonicalAppDevBlock + `
cloud-dev.loopzero.example {
  reverse_proxy @ws localhost:5173
}
`
	dir := makeCaddyfile(t, twoBlocks)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("want Pass=true when reverse_proxy is only in sibling block, got Pass=false Violations=%v",
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
