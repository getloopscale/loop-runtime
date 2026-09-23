// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package caddyfilecspgate is the Go home for the caddyfile-csp gate (migrated
// from infra/tests/check-caddyfile-app-dev-no-csp_test.sh, plan
// caddyfile-csp-vite-dev-vs-prod-bundle v1.1).
//
// Asserts the operator-local-edge `app-dev.loopzero.example { ... }` block of
// `infra/local/Caddyfile`:
//
//  1. Does NOT emit a Content-Security-Policy directive (ADR D1 — let the
//     upstream <meta> CSP in dashboard/index.html rule; browsers enforce the
//     STRICTER intersection of header CSP vs <meta> CSP).
//  2. Does NOT contain `handle /src/*`, `handle /@vite/*`, or
//     `handle /node_modules/.vite/*` 404 blocks (ADR D2 — those break Vite
//     HMR module resolution; prod info-disclosure is handled by
//     `dashboard/nginx.conf:try_files`).
//  3. Preserves `X-Frame-Options "DENY"`, `X-Content-Type-Options "nosniff"`,
//     `Referrer-Policy "strict-origin-when-cross-origin"`, HSTS (ADR D3 —
//     these don't intersect with <meta> CSP and close real attack surface;
//     #44/#45/#47 stay closed).
//  4. Preserves `reverse_proxy @ws localhost:5173` (the Vite dev upstream).
package caddyfilecspgate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qgshell"
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

// authDevBlockOpenRe finds the opening `auth-dev.loopzero.example {` line of the
// host block (T1.3 of plan kratos-self-service-rate-limit-systemic v1.1 —
// rate_limit + trusted_proxies invariants extend the gate to the auth-dev
// host beyond the existing app-dev coverage).
var authDevBlockOpenRe = regexp.MustCompile(`(?m)^auth-dev\.loopzero\.dev\s*\{`)

// canonicalRateLimitZones are the 4 zones that MUST be present in the
// auth-dev host block per T1.2 of plan kratos-self-service-rate-limit-systemic
// (closes Red Team loopzero-cloud#68 login + #89 recovery + defense-in-depth #43
// register). Per blueprint Q1 canonical snippet.
var canonicalRateLimitZones = []string{
	"zone login_throttle",
	"zone register_throttle",
	"zone recovery_throttle",
	"zone recovery_flow_create_throttle",
}

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
		token string
		why   string
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

	// Invariant 9.5 (v1.4): canonical API proxy handles MUST be present in the
	// app-dev host block. The dashboard `src/lib/api.ts` + the LoopZero CLI both
	// hit specific path prefixes that need explicit reverse-proxy handles
	// BEFORE the catch-all SPA fallback (invariant 11 below) catches them.
	// Without ANY of these, the SPA-fallback rewrite returns `<!DOCTYPE html>`
	// to consumers expecting JSON → JSON.parse → SyntaxError → consumer-blind
	// (Red Team loopzero-cloud#88 LIVE: CLI `loopzero whoami → Cannot reach API`).
	// Each invariant cites the dashboard / CLI surface that breaks.
	requiredHandles := []struct {
		path string
		why  string
	}{
		{`handle /sessions/*`, "Kratos session API (dashboard auth.getSession)"},
		{`handle /self-service/*`, "Kratos self-service flows (login/recovery/registration)"},
		{`handle /v1/*`, "loopzero-cloud API (dashboard /v1/auth/me, /v1/billing/*, /v1/dashboard/memory/*)"},
		{`handle /engine/*`, "loopzero-cloud opaque BFF proxy to engine (CLI loopzero whoami + dashboard engineFetch)"},
	}
	for _, req := range requiredHandles {
		if !strings.Contains(block, req.path) {
			out.Pass = false
			out.Violations = append(out.Violations, fmt.Sprintf(
				"assert_canonical_api_handle_present — `%s {` MISSING. Without it, requests to that prefix fall through to the SPA catch-all `handle {}` and consumers receive `<!DOCTYPE html>` instead of JSON. Surface that breaks: %s.",
				req.path, req.why))
		}
	}

	// Invariant 10 (v2): try_files {path} /index.html SPA-routing fallback
	// PRESENT (so client-side routes return the SPA's index.html).
	// Regex requires start-of-line + optional leading whitespace before the
	// directive (v1.5 tightening — closes the false positive where a
	// `try_files {path} /index.html` reference INSIDE A COMMENT line
	// `  # \`try_files {path} /index.html\` and the server returns` matched
	// the loose regex and made the invariant-11 walker report the wrong
	// enclosing handle. Comments start with `#` after leading whitespace, so
	// `^\s*try_files` never matches a commented reference).
	tryFilesRe := regexp.MustCompile(`(?m)^\s*try_files\s+\{path\}\s+/index\.html`)
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

	// T1.3 of plan kratos-self-service-rate-limit-systemic v1.1 — 3 new
	// invariants on the auth-dev block (separate from app-dev). These do NOT
	// short-circuit the existing app-dev assertions; both blocks are audited.
	authBlock, authOK := extractAuthDevBlock(content)
	if !authOK {
		// auth-dev block is required by the plan; absence is a violation.
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_auth_dev_block_present — `auth-dev.loopzero.example { ... }` host block MISSING. Plan kratos-self-service-rate-limit-systemic T1.2 declares 4 rate_limit zones MUST live inside this block.")
	} else {
		// Invariant 12 (v1.5 — T1.3): auth-dev block declares a `rate_limit { ... }`
		// container with all 4 canonical zones (login + register + recovery
		// submit + recovery flow-create). Each zone keyed on
		// `{http.request.header.cf-connecting-ip}` (EC-2 absorption — without
		// cf-connecting-ip + trusted_proxies, the bucket shares one slot across
		// all CF-fronted traffic = FAANG-grade DoS).
		rateLimitDirRe := regexp.MustCompile(`(?m)^\s*rate_limit\s*\{`)
		if !rateLimitDirRe.MatchString(authBlock) {
			out.Pass = false
			out.Violations = append(out.Violations,
				"assert_authdev_rate_limit_block_present — `rate_limit { ... }` directive MISSING in auth-dev host block. Red Team loopzero-cloud#68 login + #89 recovery email-bomb stay open without it (per blueprint Q1 canonical zones).")
		} else {
			// All 4 zones must be present.
			for _, zone := range canonicalRateLimitZones {
				if !strings.Contains(authBlock, zone) {
					out.Pass = false
					out.Violations = append(out.Violations, fmt.Sprintf(
						"assert_authdev_rate_limit_zone_%s — `%s` MISSING in auth-dev host block. All 4 canonical zones (login + register + recovery submit + recovery flow-create) are required per plan kratos-self-service-rate-limit-systemic T1.2.",
						strings.TrimPrefix(zone, "zone "), zone))
				}
			}
		}

		// Invariant 13 (v1.5 — T1.3 + EC-2): every rate_limit zone in
		// auth-dev keys on cf-connecting-ip, NOT {remote_host}.
		// {remote_host} resolves to CF tunnel's internal hop address →
		// shared bucket across all CF-fronted users → trivial DoS.
		// Defensive grep: if `key {remote_host}` appears in the auth-dev block
		// AND any rate_limit zone exists, it's a regression.
		if rateLimitDirRe.MatchString(authBlock) {
			remoteHostKeyRe := regexp.MustCompile(`(?m)^\s*key\s*\{remote_host\}`)
			if remoteHostKeyRe.MatchString(authBlock) {
				out.Pass = false
				out.Violations = append(out.Violations,
					"assert_authdev_rate_limit_key_cf_connecting_ip — `key {remote_host}` found inside auth-dev rate_limit block. EC-2 absorption: must use `key {http.request.header.cf-connecting-ip}` so per-IP buckets reflect the real end-user IP through Cloudflare Tunnel (otherwise the bucket shares one slot across all CF-fronted users = FAANG-grade DoS).")
			}
			cfKeyRe := regexp.MustCompile(`key\s*\{http\.request\.header\.cf-connecting-ip\}`)
			if !cfKeyRe.MatchString(authBlock) {
				out.Pass = false
				out.Violations = append(out.Violations,
					"assert_authdev_rate_limit_key_cf_connecting_ip — no `key {http.request.header.cf-connecting-ip}` found inside rate_limit zones. EC-2 absorption: required for per-real-IP buckets through CF Tunnel.")
			}
		}

		// Invariant 15 (v1.6 — T3.3): auth-dev block declares
		// `handle /self-service/registration/methods/password { ... }`
		// reverse-proxying to the loopzero-cloud BFF. Closes Red Team loopzero-cloud#43
		// at the routing layer — o que importa e que o registro passe pelo BFF
		// (que unifica a resposta e mata a enumeracao de contas), nao pelo Kratos
		// direto.
		//
		// A PORTA mudou de 3002 para 3002 em 2026-08-10: o B-010 trocou o
		// transporte do loopzero-cloud de `kubectl port-forward` para NodePort
		// publicado pelo loadbalancer, e a 3002 deixou de existir. O invariante e
		// o destino, e a porta e apenas o endereco dele.
		registerRouteRe := regexp.MustCompile(`(?m)^\s*handle\s+/self-service/registration/methods/password\s*\{`)
		if !registerRouteRe.MatchString(authBlock) {
			out.Pass = false
			out.Violations = append(out.Violations,
				"assert_register_route_proxied_to_cloud — `handle /self-service/registration/methods/password { ... }` MISSING in auth-dev host block. Red Team loopzero-cloud#43 closure (BFF response-shape unifier per T3.1) is unreachable without this routing handle (T3.2).")
		} else {
			registerRouteBlockRe := regexp.MustCompile(`(?ms)handle\s+/self-service/registration/methods/password\s*\{[^}]*reverse_proxy\s+localhost:3002`)
			if !registerRouteBlockRe.MatchString(authBlock) {
				out.Pass = false
				out.Violations = append(out.Violations,
					"assert_register_route_proxied_to_cloud — `handle /self-service/registration/methods/password` does NOT reverse_proxy to localhost:3002 (loopzero-cloud BFF). Red Team loopzero-cloud#43 closure requires this exact target.")
			}
		}
	}

	// Invariant 14 (v1.5 — T1.3 + EC-2): global config block declares
	// `servers { trusted_proxies static <CF-IPv4-ranges> }` so Caddy honors
	// the cf-connecting-ip header ONLY when traffic arrives from CF's IP
	// ranges (defense vs spoofed header from non-CF hop).
	trustedProxiesRe := regexp.MustCompile(`(?m)^\s*trusted_proxies\s+static\s`)
	if !trustedProxiesRe.MatchString(content) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_trusted_proxies_static_cf_ranges — global `servers { trusted_proxies static ... }` block MISSING from Caddyfile. EC-2 absorption: without trusted_proxies CF ranges, Caddy refuses to honor cf-connecting-ip header → rate_limit buckets share one slot across all CF traffic. Pin CF's published IPv4 ranges per https://www.cloudflare.com/ips/ + annual refresh.")
	} else {
		// F-arch-4 absorption: extract the trusted_proxies block (brace-balance)
		// and count CIDRs ONLY inside that block — counting CIDRs file-globally
		// allows a Caddyfile with empty trusted_proxies + CIDR comments to
		// silently satisfy the gate. F-sec-4 absorption: also count IPv6 CIDRs
		// (CF publishes BOTH IPv4 and IPv6 ranges; either family without the
		// other re-introduces the bucket-collision bypass).
		ipv4Re := regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}/\d{1,2}\b`)
		ipv6Re := regexp.MustCompile(`\b[0-9a-fA-F:]+::?(?:[0-9a-fA-F]{1,4})?/\d{1,3}\b`)
		tpBody, tpOK := extractTrustedProxiesBlock(content)
		if !tpOK {
			out.Pass = false
			out.Violations = append(out.Violations,
				"assert_trusted_proxies_block_extractable — `trusted_proxies static { ... }` body brace-balance failed. Block likely truncated or malformed.")
		} else {
			ipv4Count := len(ipv4Re.FindAllString(tpBody, -1))
			ipv6Count := len(ipv6Re.FindAllString(tpBody, -1))
			if ipv4Count < 10 {
				out.Pass = false
				out.Violations = append(out.Violations, fmt.Sprintf(
					"assert_trusted_proxies_cf_ipv4_count — only %d IPv4 CIDR(s) inside trusted_proxies block; CF publishes ~15. Plan kratos-self-service-rate-limit-systemic T1.2 requires ≥ 10 CF IPv4 CIDRs pinned.",
					ipv4Count))
			}
			if ipv6Count < 5 {
				out.Pass = false
				out.Violations = append(out.Violations, fmt.Sprintf(
					"assert_trusted_proxies_cf_ipv6_count — only %d IPv6 CIDR(s) inside trusted_proxies block; CF publishes 7. F-sec-4 absorption: pin all CF IPv6 ranges to close the IPv6-hop bypass.",
					ipv6Count))
			}
		}
	}

	return out, nil
}

// extractTrustedProxiesBlock returns the body inside `trusted_proxies static {
// ... }` OR — when the static form is used inline as `trusted_proxies static
// 1.2.3.4/16 5.6.7.0/24` (no braces, line continuations via `\`) — returns
// the contiguous line-continuation tail. F-arch-4 absorption: counting CIDRs
// only inside this block defeats the bypass where a Caddyfile sprinkles CIDR
// strings in comments while keeping trusted_proxies empty. Returns (body,
// true) on success; ("", false) when no `trusted_proxies static` is present.
// leadingBraceRe matches the first `{` of a block that opens after whitespace.
var leadingBraceRe = regexp.MustCompile(`^\s*\{`)

func extractTrustedProxiesBlock(content string) (string, bool) {
	// Two canonical shapes:
	//   (1) `trusted_proxies static <ipv4cidr> <ipv6cidr> ...` over one or more
	//       lines (line-continuation `\`).
	//   (2) `trusted_proxies static { ... }` block form (less common).
	tpRe := regexp.MustCompile(`(?m)trusted_proxies\s+static\b`)
	loc := tpRe.FindStringIndex(content)
	if loc == nil {
		return "", false
	}
	tail := content[loc[1]:]
	// Brace form?
	if i := indexNonSpace(tail); i >= 0 && tail[i] == '{' {
		return qgshell.ExtractBraceBlock(tail, leadingBraceRe)
	}
	// Line-continuation form: read until the first un-continued newline OR
	// the next directive-terminating closing brace at the same level. Treat
	// `\` followed by newline as continuation; bare newline ends the block.
	var b strings.Builder
	for j := 0; j < len(tail); j++ {
		c := tail[j]
		if c == '\n' {
			break
		}
		if c == '\\' && j+1 < len(tail) && tail[j+1] == '\n' {
			b.WriteByte(' ')
			j++ // consume newline
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

// indexNonSpace returns the index of the first non-whitespace byte in s, or -1.
func indexNonSpace(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return i
		}
	}
	return -1
}

// extractAuthDevBlock returns the body of the `auth-dev.loopzero.example { ... }`
// host block via brace-balance counting starting at the opening `{`. T1.3 of
// plan kratos-self-service-rate-limit-systemic v1.1. Returns (body, true) on
// success, ("", false) when the opening directive is missing or unbalanced.
func extractAuthDevBlock(content string) (string, bool) {
	return qgshell.ExtractBraceBlock(content, authDevBlockOpenRe)
}

// extractAppDevBlock returns the body of the `app-dev.loopzero.example { ... }`
// block via brace-balance counting starting at the opening `{`. Returns
// (body, true) on success, ("", false) when the opening directive is missing
// or the braces are unbalanced.
func extractAppDevBlock(content string) (string, bool) {
	return qgshell.ExtractBraceBlock(content, blockOpenRe)
}
