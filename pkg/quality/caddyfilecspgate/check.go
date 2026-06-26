// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package caddyfilecspgate is the Go home for the caddyfile-csp gate (migrated
// from infra/tests/check-caddyfile-app-dev-no-csp_test.sh, plan
// caddyfile-csp-vite-dev-vs-prod-bundle v1.1).
//
// Asserts the operator-local-edge `app-dev.loopzero.example { ... }` block of
// `infra/local/Caddyfile`:
//
//   1. Does NOT emit a Content-Security-Policy directive (ADR D1 — let the
//      upstream <meta> CSP in dashboard/index.html rule; browsers enforce the
//      STRICTER intersection of header CSP vs <meta> CSP).
//   2. Does NOT contain `handle /src/*`, `handle /@vite/*`, or
//      `handle /node_modules/.vite/*` 404 blocks (ADR D2 — those break Vite
//      HMR module resolution; prod info-disclosure is handled by
//      `dashboard/nginx.conf:try_files`).
//   3. Preserves `X-Frame-Options "DENY"`, `X-Content-Type-Options "nosniff"`,
//      `Referrer-Policy "strict-origin-when-cross-origin"`, HSTS (ADR D3 —
//      these don't intersect with <meta> CSP and close real attack surface;
//      #44/#45/#47 stay closed).
//   4. Preserves `reverse_proxy @ws localhost:5173` (the Vite dev upstream).
package caddyfilecspgate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Skipped    bool // Caddyfile absent
	Violations []string
}

// blockOpenRe finds the opening `app-dev.loopzero.example {` line of the host block.
var blockOpenRe = regexp.MustCompile(`(?m)^app-dev\.loopzero\.dev\s*\{`)

// Check enforces the 11 invariants per plan caddyfile-csp-vite-dev-vs-prod-bundle v1.1 T1.1.
// Returns Skipped=true when the Caddyfile is absent (CI runs without the
// operator-local-edge config are valid no-ops).
func Check(cfg Config) (*Outcome, error) {
	caddyfilePath := filepath.Join(cfg.RepoRoot, "infra", "local", "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Outcome{Pass: true, Skipped: true}, nil
		}
		return nil, fmt.Errorf("read Caddyfile: %w", err)
	}

	content := string(data)
	out := &Outcome{Pass: true}

	block, ok := extractAppDevBlock(content)
	if !ok {
		out.Pass = false
		out.Violations = append(out.Violations,
			"app-dev.loopzero.example block not found (or unbalanced braces) — block extraction failed")
		return out, nil
	}

	// Sanity (v2): the extracted block must contain `header {` (otherwise the
	// extraction grabbed the wrong span). v1 used `reverse_proxy` as the
	// sanity marker, but v2 replaces Vite-dev reverse_proxy with file_server
	// prod-bundle per ADR D1 — so the canonical block-presence marker is the
	// `header {` directive which is required for the 4 preserved security
	// headers.
	if !strings.Contains(block, "header") {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_app_dev_block_bounded — block extracted contains no `header` directive (extraction sanity-check failed)")
	}

	// Invariant 1 (v2 — supersedes v1's `assert_no_csp`): with file_server
	// serving prod-bundle, CSP IS required (asserted as invariant 8 below).
	// The v1 assertion that CSP must be ABSENT was correct ONLY when the
	// upstream was Vite dev (which needs inline scripts); with file_server,
	// the canonical posture is strict CSP per dashboard/nginx.conf:23.

	// Invariant 2: no Vite-path handle blocks (legacy v1 — still relevant as
	// defense-in-depth; with file_server they're redundant but their presence
	// would indicate someone re-introduced the prior broken topology).
	for _, vitePath := range []string{
		"handle /src/*",
		"handle /@vite/*",
		"handle /node_modules/.vite/*",
	} {
		if strings.Contains(block, vitePath) {
			out.Pass = false
			out.Violations = append(out.Violations, fmt.Sprintf(
				"assert_no_vite_handle_blocks_in_app_dev_block — `%s` STILL present", vitePath))
		}
	}

	// Invariant 3: X-Frame-Options DENY preserved (ADR D3 / #44).
	xfoRe := regexp.MustCompile(`X-Frame-Options\s+"DENY"`)
	if !xfoRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_xfo_preserved_in_app_dev_block — X-Frame-Options DENY MISSING (regression of #44)")
	}

	// Invariant 4: X-Content-Type-Options nosniff preserved (ADR D3 / #45).
	xctoRe := regexp.MustCompile(`X-Content-Type-Options\s+"nosniff"`)
	if !xctoRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_xcto_preserved_in_app_dev_block — X-Content-Type-Options nosniff MISSING (regression of #45)")
	}

	// Invariant 5: Referrer-Policy preserved (ADR D3 / #45).
	referrerRe := regexp.MustCompile(`Referrer-Policy\s+"strict-origin-when-cross-origin"`)
	if !referrerRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_referrer_preserved_in_app_dev_block — Referrer-Policy MISSING (regression of #45)")
	}

	// Invariant 6: HSTS preserved (ADR D3 / #45).
	hstsRe := regexp.MustCompile(`Strict-Transport-Security\s+"max-age=31536000`)
	if !hstsRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_hsts_preserved_in_app_dev_block — HSTS max-age=31536000 MISSING (regression of #45)")
	}

	// Invariant 7 (v2 — per plan caddyfile-app-dev-prod-bundle-file-server-canonical
	// ADR D1, supersedes v1's `assert_reverse_proxy_preserved`): the operator-
	// local Caddy MUST NOT reverse-proxy to Vite dev. The public surface MUST
	// serve the prod-bundle via file_server. Negative-direction guard closes
	// Red Team #66 (Vite source TypeScript publicly served).
	// v1.1 amendment (LIVE diagnostic 2026-06-26): widened from string-match
	// to regex to detect BOTH the named-matcher form (`reverse_proxy @ws
	// localhost:5173`) AND the catch-all form (`reverse_proxy localhost:5173`
	// at the block tail). The v1 string-match only caught the first form; the
	// catch-all leaked through and SHADOWED file_server in route order (Caddy
	// parser placed the catch-all LAST = matched first at runtime for any path
	// not handled by the explicit `handle` blocks). Red Team /src/main.tsx
	// returned 200 with Vite-dev source content.
	viteReverseProxyRe := regexp.MustCompile(`reverse_proxy\s+(@\w+\s+)?localhost:5173`)
	if viteReverseProxyRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_no_vite_dev_reverse_proxy — `reverse_proxy [@matcher] localhost:5173` STILL present (Red Team #66 regression — Vite dev MUST NOT be the public upstream; use file_server prod-bundle instead. v1.1 widened regex catches BOTH the @ws named-matcher form AND the catch-all block-tail form)")
	}

	// Invariant 8 (v2): Content-Security-Policy directive PRESENT in header
	// block.
	if !strings.Contains(block, "Content-Security-Policy") {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_csp_restored_in_app_dev_block — Content-Security-Policy directive MISSING (per ADR D2 of plan caddyfile-app-dev-prod-bundle-file-server-canonical)")
	}

	// Invariant 8.1 (v1.2): the header CSP MUST be a SUPERSET of the dashboard
	// `<meta>` CSP (browsers enforce STRICTER intersection per WHATWG fetch
	// spec). Key tokens the dashboard requires that a stricter shape would
	// strip: Google Fonts CSS + font binaries, Stripe checkout connect,
	// 'unsafe-inline' for the PWA inline script-register. Caught LIVE
	// 2026-06-26 by Red Team Chrome devtools when prior shape stripped them.
	cspMustContain := []struct {
		token  string
		why    string
	}{
		{"https://fonts.googleapis.com", "Google Fonts CSS (style-src)"},
		{"https://fonts.gstatic.com", "Google Fonts binaries (font-src)"},
		{"https://*.stripe.com", "Stripe checkout connect-src"},
		{"'unsafe-inline'", "PWA inline service-worker register + design-system CSS-in-JS"},
		{"frame-ancestors 'none'", "clickjacking defense (header-only directive)"},
	}
	for _, req := range cspMustContain {
		if !strings.Contains(block, req.token) {
			out.Pass = false
			out.Violations = append(out.Violations, fmt.Sprintf(
				"assert_csp_superset_of_meta — header CSP MISSING `%s` (%s). Stricter header CSP intersects with dashboard meta CSP and strips this source, breaking the page.",
				req.token, req.why))
		}
	}

	// Invariant 9 (v2): file_server directive PRESENT (the canonical upstream
	// for the prod-bundle topology per ADR D1).
	if !strings.Contains(block, "file_server") {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_file_server_in_app_dev_block — `file_server` directive MISSING (per ADR D1: app-dev serves prod-bundle via file_server, NOT Vite-dev reverse-proxy)")
	}

	// Invariant 10 (v2): try_files {path} /index.html SPA-routing fallback
	// PRESENT (so client-side routes return the SPA's index.html).
	tryFilesRe := regexp.MustCompile(`try_files\s+\{path\}\s+/index\.html`)
	if !tryFilesRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_spa_fallback_try_files — `try_files {path} /index.html` MISSING (SPA client-side routes would 404; per ADR D1)")
	}

	// Invariant 11 (v1.3): the SPA-fallback `try_files` MUST live inside an
	// unnamed catch-all `handle {}` block, NOT at the top level of the host
	// block. At the top level, `try_files` is in the "rewrite" directive
	// group and runs BEFORE the routing phase, so it rewrites EVERY non-
	// existent path (including /sessions/whoami, /v1/auth/me) to /index.html
	// — the explicit `handle /sessions/*` + `handle /v1/*` reverse-proxies
	// never see those requests and the dashboard JSON.parse's SPA HTML as
	// auth responses. Browser-LIVE-caught 2026-06-26 by Red Team Chrome
	// devtools: `[auth] getSession: network/parse error SyntaxError:
	// Unexpected token '<', "<!DOCTYPE "... is not valid JSON`. The
	// canonical Caddy v2 SPA + API proxy pattern wraps file_server +
	// try_files in an unnamed `handle {}` block at the end so the SPA
	// rewrite only applies to paths that fell through every explicit
	// handle. Detection: find `try_files {path} /index.html` and walk
	// backward looking for the nearest enclosing `handle` block opener.
	tryFilesIdx := tryFilesRe.FindStringIndex(block)
	if tryFilesIdx != nil {
		// Find the nearest preceding `handle` or `handle <matcher>` line.
		preceding := block[:tryFilesIdx[0]]
		handleOpenRe := regexp.MustCompile(`(?m)^\s*handle(\s+[^\s{]*)?\s*\{`)
		handleOpens := handleOpenRe.FindAllStringIndex(preceding, -1)
		if len(handleOpens) == 0 {
			out.Pass = false
			out.Violations = append(out.Violations,
				"assert_try_files_inside_handle_catchall — `try_files {path} /index.html` is at the TOP LEVEL of the app-dev block (no enclosing `handle {}`). Caddy v2 evaluates `try_files` as a rewrite that runs BEFORE the routing phase, so it rewrites EVERY non-existent path (including /sessions/whoami + /v1/auth/me) to /index.html — explicit reverse-proxy handles never see those requests and the dashboard JSON.parse's SPA HTML as auth responses (Red Team LIVE-caught 2026-06-26). Wrap file_server + try_files in an unnamed `handle {}` at the end of the block.")
		} else {
			// Found at least one handle opener before try_files. Check that
			// the IMMEDIATELY preceding open handle has no matcher (i.e., is
			// the unnamed catch-all `handle {`) — anything else would mean
			// try_files is inside e.g. `handle /v1/*`, which is wrong too.
			lastOpen := handleOpens[len(handleOpens)-1]
			openerLine := strings.TrimSpace(preceding[lastOpen[0]:lastOpen[1]])
			if openerLine != "handle {" {
				out.Pass = false
				out.Violations = append(out.Violations, fmt.Sprintf(
					"assert_try_files_inside_unnamed_handle — `try_files {path} /index.html` is inside `%s` (a path-matched handle). The SPA-fallback rewrite would only apply to that path, not as a catch-all. Move it inside `handle { ... }` (unnamed catch-all) at the end of the block.",
					openerLine))
			}
		}
	}

	return out, nil
}

// extractAppDevBlock returns the body of the `app-dev.loopzero.example { ... }`
// block via brace-balance counting starting at the opening `{`. Returns
// (body, true) on success, ("", false) when the opening directive is missing
// or the braces are unbalanced.
func extractAppDevBlock(content string) (string, bool) {
	openLoc := blockOpenRe.FindStringIndex(content)
	if openLoc == nil {
		return "", false
	}

	// Start scanning right after the opening `{`.
	depth := 1
	start := openLoc[1]
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start:i], true
			}
		}
	}
	// Unbalanced braces.
	return "", false
}
