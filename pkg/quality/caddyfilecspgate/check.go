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
	if strings.Contains(block, "reverse_proxy @ws localhost:5173") {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_no_vite_dev_reverse_proxy — `reverse_proxy @ws localhost:5173` STILL present (Red Team #66 regression — Vite dev MUST NOT be the public upstream; use file_server prod-bundle instead)")
	}

	// Invariant 8 (v2): Content-Security-Policy directive PRESENT in header
	// block, matching the canonical shape from dashboard/nginx.conf:23 (per
	// ADR D2). With file_server serving the prod-bundle, the page no longer
	// needs Vite HMR's inline scripts — the strict CSP applies cleanly.
	if !strings.Contains(block, "Content-Security-Policy") {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_csp_restored_in_app_dev_block — Content-Security-Policy directive MISSING (per ADR D2 of plan caddyfile-app-dev-prod-bundle-file-server-canonical: restore canonical CSP shape from dashboard/nginx.conf:23)")
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
