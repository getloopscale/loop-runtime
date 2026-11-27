// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

// canonicalGlobalConfigBlock is the canonical Caddyfile global config —
// v1.6 (T1.3 + EC-2 + F-sec-4): trusted_proxies declares CF's published IPv4
// AND IPv6 ranges so Caddy honors the cf-connecting-ip header from CF
// tunnel only (covers BOTH families — IPv6 bypass closed per F-sec-4).
const canonicalGlobalConfigBlock = `{
  email admin@getloopscale.com
  servers {
    trusted_proxies static \
      173.245.48.0/20 \
      103.21.244.0/22 \
      103.22.200.0/22 \
      103.31.4.0/22 \
      141.101.64.0/18 \
      108.162.192.0/18 \
      190.93.240.0/20 \
      188.114.96.0/20 \
      197.234.240.0/22 \
      198.41.128.0/17 \
      162.158.0.0/15 \
      104.16.0.0/13 \
      104.24.0.0/14 \
      172.64.0.0/13 \
      131.0.72.0/22 \
      2400:cb00::/32 \
      2606:4700::/32 \
      2803:f800::/32 \
      2405:b500::/32 \
      2405:8100::/32 \
      2a06:98c0::/29 \
      2c0f:f248::/32
  }
}
`

// canonicalAuthDevBlock is the v1.6 (T1.2 + T3.2 of plan
// kratos-self-service-rate-limit-systemic) canonical shape: 4 rate_limit zones
// keyed on cf-connecting-ip + register-route handle proxied to loop-console BFF
// + HSTS header preserved.
const canonicalAuthDevBlock = `auth-dev.getloopscale.com {
  header Strict-Transport-Security "max-age=31536000; includeSubDomains"

  rate_limit {
    zone login_throttle {
      match {
        path /self-service/login/methods/*
      }
      key {http.request.header.cf-connecting-ip}
      window 1m
      events 10
    }
    zone register_throttle {
      match {
        path /self-service/registration/methods/*
      }
      key {http.request.header.cf-connecting-ip}
      window 1m
      events 5
    }
    zone recovery_throttle {
      match {
        path /self-service/recovery/methods/*
      }
      key {http.request.header.cf-connecting-ip}
      window 1m
      events 3
    }
    zone recovery_flow_create_throttle {
      match {
        path /self-service/recovery/browser /self-service/recovery/api
      }
      key {http.request.header.cf-connecting-ip}
      window 5m
      events 5
    }
  }

  handle /self-service/registration/methods/password {
    reverse_proxy localhost:3002 {
      header_up Host cloud-dev.getloopscale.com
    }
  }

  reverse_proxy localhost:14433
}
`

// canonicalAppDevBlock is the v2 (file_server prod-bundle) canonical shape per
// plan caddyfile-app-dev-prod-bundle-file-server-canonical ADR D1+D2, paired
// with the v1.5 global config + auth-dev block per T1.3.
const canonicalAppDevBlock = canonicalGlobalConfigBlock + `
app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' data: https://fonts.gstatic.com; img-src 'self' data: blob: https:; connect-src 'self' ws: wss: https://checkout.stripe.com https://*.stripe.com https://auth-dev.getloopscale.com https://cloud-dev.getloopscale.com; form-action 'self' https://auth-dev.getloopscale.com https://accounts.google.com https://github.com; base-uri 'self'; object-src 'none'; frame-ancestors 'none'"
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
  }

  handle /sessions/* {
    reverse_proxy localhost:14433
  }
  handle /self-service/* {
    reverse_proxy localhost:14433
  }
  handle /v1/* {
    reverse_proxy localhost:3002
  }
  handle /engine/* {
    reverse_proxy localhost:3002
  }

  handle {
    root * {env.DASHBOARD_DIST}
    encode gzip
    file_server
    try_files {path} /index.html
  }
}
` + canonicalAuthDevBlock

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
		`Content-Security-Policy "default-src 'self'`, `Forbidden-Policy "stripped`, 1)
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

func TestCheck_RegressionDetected_CSPStripperShape_v12(t *testing.T) {
	// v1.2: header CSP MUST be a SUPERSET of dashboard meta CSP. A canonical-
	// looking but-narrower CSP (no Google Fonts) MUST fail the superset gate.
	// Browser-LIVE-caught 2026-06-26 by Red Team Chrome devtools.
	narrowerCSP := `Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self' https://auth-dev.getloopscale.com https://cloud-dev.getloopscale.com; object-src 'none'; base-uri 'self'; form-action 'self' https://auth-dev.getloopscale.com https://accounts.google.com https://github.com; frame-ancestors 'none'"`
	regressed := strings.Replace(canonicalAppDevBlock,
		`Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' data: https://fonts.gstatic.com; img-src 'self' data: blob: https:; connect-src 'self' ws: wss: https://checkout.stripe.com https://*.stripe.com https://auth-dev.getloopscale.com https://cloud-dev.getloopscale.com; form-action 'self' https://auth-dev.getloopscale.com https://accounts.google.com https://github.com; base-uri 'self'; object-src 'none'; frame-ancestors 'none'"`,
		narrowerCSP, 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when CSP is narrower than meta CSP (would strip Google Fonts via intersection)")
	}
	wantTokens := []string{"https://fonts.googleapis.com", "https://fonts.gstatic.com", "https://*.stripe.com"}
	for _, tok := range wantTokens {
		found := false
		for _, v := range out.Violations {
			if strings.Contains(v, tok) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("want superset violation citing %q, got: %v", tok, out.Violations)
		}
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

// TestCheck_RegressionDetected_MissingEngineHandle_v14 locks the v1.4 fix
// for Red Team loop-console#88 LIVE regression: the canonical handle blocks
// for the dashboard + CLI API prefixes (/sessions/*, /self-service/*,
// /v1/*, /engine/*) MUST all be present. Without /engine/*, every CLI
// `loopscale whoami` AND every dashboard `engineFetch()` call falls through
// to the SPA catch-all and the consumer receives `<!DOCTYPE html>` instead
// of JSON. Defense-in-depth invariant — the gate enumerates EVERY required
// API handle so a future canonical-paths refactor cannot silently drop one.
func TestCheck_RegressionDetected_MissingEngineHandle_v14(t *testing.T) {
	regressed := strings.Replace(canonicalAppDevBlock,
		"  handle /engine/* {\n    reverse_proxy localhost:3002\n  }\n", "", 1)
	dir := makeCaddyfile(t, regressed)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when /engine/* handle is missing (CLI + dashboard engineFetch break)")
	}
	found := false
	for _, v := range out.Violations {
		if strings.Contains(v, "handle /engine/*") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("want violation citing missing /engine/* handle, got: %v", out.Violations)
	}
}

// TestCheck_RegressionDetected_TryFilesAtTopLevel_v13 locks the v1.3 fix
// for the dashboard `[auth] getSession: ... Unexpected token '<' is not
// valid JSON` LIVE regression. The original v1.2 shape had file_server +
// try_files at the TOP LEVEL of the app-dev host block — Caddy v2's
// rewrite phase rewrote every non-existent path (including /sessions/whoami
// and /v1/auth/me) to /index.html BEFORE the explicit `handle /sessions/*`
// + `handle /v1/*` reverse-proxies could match, so the dashboard JSON.parse'd
// SPA HTML as auth responses. The canonical fix wraps file_server +
// try_files in an unnamed catch-all `handle {}` block at the end so the
// SPA-fallback rewrite only applies after every explicit handle missed.
func TestCheck_RegressionDetected_TryFilesAtTopLevel_v13(t *testing.T) {
	regressedTopLevel := `app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' data: https://fonts.gstatic.com; img-src 'self' data: blob: https:; connect-src 'self' ws: wss: https://checkout.stripe.com https://*.stripe.com https://auth-dev.getloopscale.com https://cloud-dev.getloopscale.com; form-action 'self' https://auth-dev.getloopscale.com https://accounts.google.com https://github.com; base-uri 'self'; object-src 'none'; frame-ancestors 'none'"
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
  }

  root * {env.DASHBOARD_DIST}
  encode gzip
  file_server
  try_files {path} /index.html

  handle /sessions/* {
    reverse_proxy localhost:14433
  }
}
`
	dir := makeCaddyfile(t, regressedTopLevel)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when try_files is at top level (would rewrite /sessions/* before handle could match), got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_try_files_inside_handle_catchall") {
		t.Fatalf("want top-level try_files violation, got: %v", out.Violations)
	}
}

// Regression-protector (v1.5, 2026-07-01): a `try_files {path} /index.html`
// substring inside a COMMENT line MUST NOT match the tryFilesRe regex — pre-
// fix the loose regex `try_files\s+\{path\}\s+/index\.html` picked up the
// commented reference at `# \`try_files {path} /index.html\` and the server
// returns` (line 274 of the production Caddyfile) as if it were the real
// directive, then walked backward and reported the nearest enclosing handle
// (`handle /engine/*`) as the — wrong — enclosing block. Fix: the regex now
// anchors on `^\s*try_files` (start-of-line + optional whitespace), which
// never matches a `#`-prefixed comment line. This test verifies the fix by
// pairing a commented reference (inside `handle /engine/*`) with the real
// directive (inside `handle {}`); pre-fix this Caddyfile would FAIL with the
// wrong enclosing handle; post-fix it PASSES cleanly.
func TestCheck_TryFilesCommentReferenceMustNotFalsePositive_v15(t *testing.T) {
	// The commented reference lives INSIDE handle /engine/* — pre-fix, this
	// tripped the walker into reporting /engine/* as the enclosing handle for
	// the (real) try_files that actually lives inside handle {} below.
	caddyfileWithCommentedTryFiles := canonicalGlobalConfigBlock + `
app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' data: https://fonts.gstatic.com; img-src 'self' data: blob: https:; connect-src 'self' ws: wss: https://checkout.stripe.com https://*.stripe.com https://auth-dev.getloopscale.com https://cloud-dev.getloopscale.com; form-action 'self' https://auth-dev.getloopscale.com https://accounts.google.com https://github.com; base-uri 'self'; object-src 'none'; frame-ancestors 'none'"
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
  }

  handle /sessions/* {
    reverse_proxy localhost:14433
  }
  handle /self-service/* {
    reverse_proxy localhost:14433
  }
  handle /v1/* {
    reverse_proxy localhost:3002
  }
  handle /engine/* {
    reverse_proxy localhost:3002
    # NOTE: the SPA-fallback ` + "`try_files {path} /index.html`" + ` is deliberately
    # NOT declared inside this /engine/* handle — it lives in the catch-all
    # ` + "`handle {}`" + ` at the end of the block. This comment used to false-positive
    # the invariant-11 walker under the pre-v1.5 loose regex.
  }

  handle {
    root * {env.DASHBOARD_DIST}
    encode gzip
    file_server
    try_files {path} /index.html
  }
}
` + canonicalAuthDevBlock
	dir := makeCaddyfile(t, caddyfileWithCommentedTryFiles)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("want Pass=true when try_files is commented in handle /engine/* AND real try_files lives in handle {} — got Pass=false Violations=%v", out.Violations)
	}
}

func TestCheck_BlockNotFound(t *testing.T) {
	dir := makeCaddyfile(t, `# no app-dev block here
cloud-dev.getloopscale.com {
  reverse_proxy localhost:3002
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
cloud-dev.getloopscale.com {
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

// ----------------------------------------------------------------------------
// T1.3 of plan kratos-self-service-rate-limit-systemic v1.1 — auth-dev rate_limit
// + trusted_proxies invariants (12 / 13 / 14). 3 new regression-detected tests.
// ----------------------------------------------------------------------------

func TestCheck_RegressionDetected_AuthDevRateLimitBlockMissing(t *testing.T) {
	// auth-dev block exists but the rate_limit { ... } container is absent.
	missingRateLimit := canonicalGlobalConfigBlock + `
app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'"
    Strict-Transport-Security "max-age=31536000"
  }
  handle /sessions/* { reverse_proxy localhost:14433 }
  handle /self-service/* { reverse_proxy localhost:14433 }
  handle /v1/* { reverse_proxy localhost:3002 }
  handle /engine/* { reverse_proxy localhost:3002 }
  handle { root * /dist; encode gzip; file_server; try_files {path} /index.html }
}

auth-dev.getloopscale.com {
  header Strict-Transport-Security "max-age=31536000"
  reverse_proxy localhost:14433
}
`
	dir := makeCaddyfile(t, missingRateLimit)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when auth-dev rate_limit block is missing, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_authdev_rate_limit_block_present") {
		t.Fatalf("want rate_limit-block-missing violation, got: %v", out.Violations)
	}
}

func TestCheck_RegressionDetected_AuthDevRateLimitZoneMissing(t *testing.T) {
	// rate_limit block exists but the register_throttle zone is absent.
	missingZone := canonicalGlobalConfigBlock + `
app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'"
    Strict-Transport-Security "max-age=31536000"
  }
  handle /sessions/* { reverse_proxy localhost:14433 }
  handle /self-service/* { reverse_proxy localhost:14433 }
  handle /v1/* { reverse_proxy localhost:3002 }
  handle /engine/* { reverse_proxy localhost:3002 }
  handle { root * /dist; encode gzip; file_server; try_files {path} /index.html }
}

auth-dev.getloopscale.com {
  header Strict-Transport-Security "max-age=31536000"
  rate_limit {
    zone login_throttle { match { path /self-service/login/methods/* }; key {http.request.header.cf-connecting-ip}; window 1m; events 10 }
    zone recovery_throttle { match { path /self-service/recovery/methods/* }; key {http.request.header.cf-connecting-ip}; window 1m; events 3 }
    zone recovery_flow_create_throttle { match { path /self-service/recovery/browser /self-service/recovery/api }; key {http.request.header.cf-connecting-ip}; window 5m; events 5 }
  }
  reverse_proxy localhost:14433
}
`
	dir := makeCaddyfile(t, missingZone)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when register_throttle zone is missing, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_authdev_rate_limit_zone_register_throttle") {
		t.Fatalf("want register_throttle-zone-missing violation, got: %v", out.Violations)
	}
}

// T3.3 of plan kratos-self-service-rate-limit-systemic v1.1 — register-route
// proxy invariant for the auth-dev block (BFF wiring per T3.2).
func TestCheck_RegressionDetected_RegisterRouteMissing(t *testing.T) {
	// auth-dev block exists with full rate_limit + handles, but the
	// /self-service/registration/methods/password handle is absent.
	missingRegisterHandle := canonicalGlobalConfigBlock + `
app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'"
    Strict-Transport-Security "max-age=31536000"
  }
  handle /sessions/* { reverse_proxy localhost:14433 }
  handle /self-service/* { reverse_proxy localhost:14433 }
  handle /v1/* { reverse_proxy localhost:3002 }
  handle /engine/* { reverse_proxy localhost:3002 }
  handle { root * /dist; encode gzip; file_server; try_files {path} /index.html }
}

auth-dev.getloopscale.com {
  header Strict-Transport-Security "max-age=31536000"
  rate_limit {
    zone login_throttle { match { path /self-service/login/methods/* }; key {http.request.header.cf-connecting-ip}; window 1m; events 10 }
    zone register_throttle { match { path /self-service/registration/methods/* }; key {http.request.header.cf-connecting-ip}; window 1m; events 5 }
    zone recovery_throttle { match { path /self-service/recovery/methods/* }; key {http.request.header.cf-connecting-ip}; window 1m; events 3 }
    zone recovery_flow_create_throttle { match { path /self-service/recovery/browser /self-service/recovery/api }; key {http.request.header.cf-connecting-ip}; window 5m; events 5 }
  }
  reverse_proxy localhost:14433
}
`
	dir := makeCaddyfile(t, missingRegisterHandle)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when register route is missing, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_register_route_proxied_to_cloud") {
		t.Fatalf("want register-route-missing violation, got: %v", out.Violations)
	}
}

func TestCheck_RegressionDetected_TrustedProxiesMissing(t *testing.T) {
	// Global config block exists but no trusted_proxies static.
	missingTP := `{
  email admin@getloopscale.com
}
` + `
app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Content-Security-Policy "default-src 'self'"
    Strict-Transport-Security "max-age=31536000"
  }
  handle /sessions/* { reverse_proxy localhost:14433 }
  handle /self-service/* { reverse_proxy localhost:14433 }
  handle /v1/* { reverse_proxy localhost:3002 }
  handle /engine/* { reverse_proxy localhost:3002 }
  handle { root * /dist; encode gzip; file_server; try_files {path} /index.html }
}
` + canonicalAuthDevBlock
	dir := makeCaddyfile(t, missingTP)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when trusted_proxies is missing, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_trusted_proxies_static_cf_ranges") {
		t.Fatalf("want trusted_proxies-missing violation, got: %v", out.Violations)
	}
}

// F-arch-4 regression-protector: an empty trusted_proxies block with CIDRs
// only in comments must FAIL the gate. The old file-global CIDR count was
// the bypass; the new block-scoped count closes it.
func TestCheck_RegressionDetected_EmptyTrustedProxiesWithCIDRCommentsBypass(t *testing.T) {
	bypass := `{
  email admin@getloopscale.com
  servers {
    # CF ranges as of 2026-06-26 (comment-only, no real entries):
    # 173.245.48.0/20 103.21.244.0/22 103.22.200.0/22 103.31.4.0/22
    # 141.101.64.0/18 108.162.192.0/18 190.93.240.0/20 188.114.96.0/20
    # 197.234.240.0/22 198.41.128.0/17 162.158.0.0/15 104.16.0.0/13
    # 104.24.0.0/14 172.64.0.0/13 131.0.72.0/22
    trusted_proxies static 1.2.3.4/32
  }
}
` + canonicalAppDevBlock + canonicalAuthDevBlock
	dir := makeCaddyfile(t, bypass)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when trusted_proxies has only 1 IPv4 CIDR (15 in comments don't count), got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_trusted_proxies_cf_ipv4_count") {
		t.Fatalf("want ipv4-count-low violation (block-scoped), got: %v", out.Violations)
	}
}

// F-sec-4 regression-protector: trusted_proxies with full IPv4 set but ZERO
// IPv6 CIDRs must FAIL. The IPv6-bypass closure is non-optional.
func TestCheck_RegressionDetected_TrustedProxiesMissingIPv6Bypass(t *testing.T) {
	missingIPv6 := `{
  email admin@getloopscale.com
  servers {
    trusted_proxies static \
      173.245.48.0/20 \
      103.21.244.0/22 \
      103.22.200.0/22 \
      103.31.4.0/22 \
      141.101.64.0/18 \
      108.162.192.0/18 \
      190.93.240.0/20 \
      188.114.96.0/20 \
      197.234.240.0/22 \
      198.41.128.0/17 \
      162.158.0.0/15 \
      104.16.0.0/13 \
      104.24.0.0/14 \
      172.64.0.0/13 \
      131.0.72.0/22
  }
}
` + canonicalAppDevBlock + canonicalAuthDevBlock
	dir := makeCaddyfile(t, missingIPv6)
	out, err := Check(Config{RepoRoot: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("want Pass=false when trusted_proxies has zero IPv6 CIDRs, got Pass=true")
	}
	if !containsViolation(out.Violations, "assert_trusted_proxies_cf_ipv6_count") {
		t.Fatalf("want ipv6-count-low violation, got: %v", out.Violations)
	}
}
