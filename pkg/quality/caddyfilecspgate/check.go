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

	// Sanity: the extracted block must contain `reverse_proxy` (otherwise the
	// extraction grabbed the wrong span).
	if !strings.Contains(block, "reverse_proxy") {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_app_dev_block_bounded — block extracted contains no `reverse_proxy` (extraction sanity-check failed)")
	}

	// Invariant 1: no Content-Security-Policy directive (ADR D1).
	if strings.Contains(block, "Content-Security-Policy") {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_no_csp_in_app_dev_block — Content-Security-Policy directive STILL present in app-dev block")
	}

	// Invariant 2: no Vite-path handle blocks (ADR D2).
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

	// Invariant 7: reverse_proxy @ws localhost:5173 preserved exactly once
	// (the Vite dev upstream).
	proxyCount := strings.Count(block, "reverse_proxy @ws localhost:5173")
	if proxyCount != 1 {
		out.Pass = false
		out.Violations = append(out.Violations, fmt.Sprintf(
			"assert_reverse_proxy_preserved — expected exactly 1 `reverse_proxy @ws localhost:5173`, got %d",
			proxyCount))
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
