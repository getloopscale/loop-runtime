// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package caddyfilecspgate is the Go home for the caddyfile-csp gate (migrated
// from infra/tests/check-caddyfile-app-dev-no-csp_test.sh, plan
// caddyfile-csp-vite-dev-vs-prod-bundle).
//
// It audits the operator-local edge in `infra/local/Caddyfile`:
//
//   - the `app-dev.getloopscale.com { ... }` block serves the prod bundle through
//     file_server (never Vite dev), keeps the security headers and a header
//     CSP that is a superset of the dashboard <meta> CSP, routes the API
//     prefixes before the SPA fallback, and keeps that fallback inside the
//     unnamed catch-all `handle {}`;
//   - the `auth-dev.getloopscale.com { ... }` block rate-limits the Kratos
//     self-service flows per real client IP and routes registration through
//     the loopruntime-cloud BFF;
//   - the global `trusted_proxies static` block pins Cloudflare's IPv4 and
//     IPv6 ranges.
package caddyfilecspgate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qgshell"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

// blockOpenRe finds the opening `app-dev.getloopscale.com {` line of the host block.
var blockOpenRe = regexp.MustCompile(`(?m)^app-dev\.getloopscale\.com\s*\{`)

// authDevBlockOpenRe finds the opening `auth-dev.getloopscale.com {` line of the
// host block (T1.3 of plan kratos-self-service-rate-limit-systemic v1.1 —
// rate_limit + trusted_proxies invariants extend the gate to the auth-dev
// host beyond the existing app-dev coverage).
var authDevBlockOpenRe = regexp.MustCompile(`(?m)^auth-dev\.getloopscale\.com\s*\{`)

// canonicalRateLimitZones are the 4 zones that MUST be present in the
// auth-dev host block per T1.2 of plan kratos-self-service-rate-limit-systemic
// (closes Red Team loopruntime-cloud#68 login + #89 recovery + defense-in-depth #43
// register). Per blueprint Q1 canonical snippet.
var canonicalRateLimitZones = []string{
	"zone login_throttle",
	"zone register_throttle",
	"zone recovery_throttle",
	"zone recovery_flow_create_throttle",
}

// Check audits the app-dev host block, the auth-dev host block and the global
// trusted_proxies block of infra/local/Caddyfile, per plans
// caddyfile-csp-vite-dev-vs-prod-bundle and kratos-self-service-rate-limit-systemic.
// Returns Skipped=true when the Caddyfile is absent (CI runs without the
// operator-local-edge config are valid no-ops). An app-dev block that cannot
// be extracted is reported alone: the other blocks are not audited then.
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

	block, ok := extractAppDevBlock(content)
	if !ok {
		return &Outcome{Violations: []string{
			"app-dev.getloopscale.com block not found (or unbalanced braces) — block extraction failed",
		}}, nil
	}

	var violations []string
	violations = append(violations, checkAppDevBlock(block)...)
	violations = append(violations, checkAuthDevBlock(content)...)
	violations = append(violations, checkTrustedProxies(content)...)
	return &Outcome{Pass: len(violations) == 0, Violations: violations}, nil
}

// blockRule is one invariant over a host block: the block violates it when
// the pattern's presence differs from want.
type blockRule struct {
	pattern *regexp.Regexp
	want    bool
	message string
}

func (r blockRule) violated(block string) bool {
	return r.pattern.MatchString(block) != r.want
}

// literal matches s verbatim, for invariants stated as a plain substring.
func literal(s string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(s))
}

// appDevRules are the app-dev invariants, in reporting order.
var appDevRules = []blockRule{
	// Sanity: the extracted block must contain `header` — otherwise the
	// extraction grabbed the wrong span. `header {` carries the four preserved
	// security headers, so it is the canonical block-presence marker.
	{literal("header"), true, "assert_app_dev_block_bounded — block extracted contains no `header` directive (extraction sanity-check failed)"},

	// No Vite-path handle blocks. Redundant with file_server, but their
	// presence means someone re-introduced the prior broken topology (ADR D2).
	{literal("handle /src/*"), false, "assert_no_vite_handle_blocks_in_app_dev_block — `handle /src/*` STILL present"},
	{literal("handle /@vite/*"), false, "assert_no_vite_handle_blocks_in_app_dev_block — `handle /@vite/*` STILL present"},
	{literal("handle /node_modules/.vite/*"), false, "assert_no_vite_handle_blocks_in_app_dev_block — `handle /node_modules/.vite/*` STILL present"},

	// Security headers that do not intersect with the <meta> CSP (ADR D3).
	{regexp.MustCompile(`X-Frame-Options\s+"DENY"`), true, "assert_xfo_preserved_in_app_dev_block — X-Frame-Options DENY MISSING (regression of #44)"},
	{regexp.MustCompile(`X-Content-Type-Options\s+"nosniff"`), true, "assert_xcto_preserved_in_app_dev_block — X-Content-Type-Options nosniff MISSING (regression of #45)"},
	{regexp.MustCompile(`Referrer-Policy\s+"strict-origin-when-cross-origin"`), true, "assert_referrer_preserved_in_app_dev_block — Referrer-Policy MISSING (regression of #45)"},
	{regexp.MustCompile(`Strict-Transport-Security\s+"max-age=31536000`), true, "assert_hsts_preserved_in_app_dev_block — HSTS max-age=31536000 MISSING (regression of #45)"},

	// The public surface serves the prod bundle, never Vite dev (Red Team
	// #66). The regex catches both the named-matcher form and the catch-all
	// block-tail form, which Caddy orders LAST and so shadows file_server.
	{regexp.MustCompile(`reverse_proxy\s+(@\w+\s+)?localhost:5173`), false, "assert_no_vite_dev_reverse_proxy — `reverse_proxy [@matcher] localhost:5173` STILL present (Red Team #66 regression — Vite dev MUST NOT be the public upstream; use file_server prod-bundle instead. v1.1 widened regex catches BOTH the @ws named-matcher form AND the catch-all block-tail form)"},
	{literal("Content-Security-Policy"), true, "assert_csp_restored_in_app_dev_block — Content-Security-Policy directive MISSING (per ADR D2 of plan caddyfile-app-dev-prod-bundle-file-server-canonical)"},
}

// cspSupersetTokens are the sources the dashboard <meta> CSP needs. Browsers
// enforce the STRICTER intersection of header and <meta> CSP, so a header CSP
// missing one of them strips it from the page.
var cspSupersetTokens = []struct {
	token string
	why   string
}{
	{"https://fonts.googleapis.com", "Google Fonts CSS (style-src)"},
	{"https://fonts.gstatic.com", "Google Fonts binaries (font-src)"},
	{"https://*.stripe.com", "Stripe checkout connect-src"},
	{"'unsafe-inline'", "PWA inline service-worker register + design-system CSS-in-JS"},
	{"frame-ancestors 'none'", "clickjacking defense (header-only directive)"},
}

// fileServerRule: app-dev serves the prod bundle via file_server (ADR D1).
var fileServerRule = blockRule{literal("file_server"), true, "assert_file_server_in_app_dev_block — `file_server` directive MISSING (per ADR D1: app-dev serves prod-bundle via file_server, NOT Vite-dev reverse-proxy)"}

// requiredAPIHandles must precede the SPA catch-all: without one of them,
// requests to that prefix receive `<!DOCTYPE html>` instead of JSON (Red Team
// loopruntime-cloud#88). Each entry names the consumer surface that breaks.
var requiredAPIHandles = []struct {
	path string
	why  string
}{
	{`handle /sessions/*`, "Kratos session API (dashboard auth.getSession)"},
	{`handle /self-service/*`, "Kratos self-service flows (login/recovery/registration)"},
	{`handle /v1/*`, "loopruntime-cloud API (dashboard /v1/auth/me, /v1/billing/*, /v1/dashboard/memory/*)"},
	{`handle /engine/*`, "loopruntime-cloud opaque BFF proxy to engine (CLI loopruntime whoami + dashboard engineFetch)"},
}

// tryFilesRe finds the SPA fallback. It is anchored at the start of a line so
// a commented reference to the directive never matches.
var tryFilesRe = regexp.MustCompile(`(?m)^\s*try_files\s+\{path\}\s+/index\.html`)

// handleOpenRe finds a `handle` or `handle <matcher>` block opener.
var handleOpenRe = regexp.MustCompile(`(?m)^\s*handle(\s+[^\s{]*)?\s*\{`)

func checkAppDevBlock(block string) []string {
	var violations []string
	for _, r := range appDevRules {
		if r.violated(block) {
			violations = append(violations, r.message)
		}
	}
	for _, req := range cspSupersetTokens {
		if !strings.Contains(block, req.token) {
			violations = append(violations, fmt.Sprintf(
				"assert_csp_superset_of_meta — header CSP MISSING `%s` (%s). Stricter header CSP intersects with dashboard meta CSP and strips this source, breaking the page.",
				req.token, req.why))
		}
	}
	if fileServerRule.violated(block) {
		violations = append(violations, fileServerRule.message)
	}
	for _, req := range requiredAPIHandles {
		if !strings.Contains(block, req.path) {
			violations = append(violations, fmt.Sprintf(
				"assert_canonical_api_handle_present — `%s {` MISSING. Without it, requests to that prefix fall through to the SPA catch-all `handle {}` and consumers receive `<!DOCTYPE html>` instead of JSON. Surface that breaks: %s.",
				req.path, req.why))
		}
	}
	return append(violations, checkSPAFallback(block)...)
}

// checkSPAFallback asserts `try_files {path} /index.html` exists and lives
// inside the unnamed catch-all `handle {}`. At the top level of the host block
// try_files is a rewrite that runs BEFORE routing, so it rewrites every
// non-existent path — /sessions/whoami and /v1/auth/me included — to
// /index.html, and the explicit reverse-proxy handles never see them. Inside a
// path-matched handle it applies to that path only.
func checkSPAFallback(block string) []string {
	loc := tryFilesRe.FindStringIndex(block)
	if loc == nil {
		return []string{"assert_spa_fallback_try_files — `try_files {path} /index.html` MISSING (SPA client-side routes would 404; per ADR D1)"}
	}
	preceding := block[:loc[0]]
	opens := handleOpenRe.FindAllStringIndex(preceding, -1)
	if len(opens) == 0 {
		return []string{"assert_try_files_inside_handle_catchall — `try_files {path} /index.html` is at the TOP LEVEL of the app-dev block (no enclosing `handle {}`). Caddy v2 evaluates `try_files` as a rewrite that runs BEFORE the routing phase, so it rewrites EVERY non-existent path (including /sessions/whoami + /v1/auth/me) to /index.html — explicit reverse-proxy handles never see those requests and the dashboard JSON.parse's SPA HTML as auth responses (Red Team LIVE-caught 2026-06-26). Wrap file_server + try_files in an unnamed `handle {}` at the end of the block."}
	}
	last := opens[len(opens)-1]
	opener := strings.TrimSpace(preceding[last[0]:last[1]])
	if opener != "handle {" {
		return []string{fmt.Sprintf(
			"assert_try_files_inside_unnamed_handle — `try_files {path} /index.html` is inside `%s` (a path-matched handle). The SPA-fallback rewrite would only apply to that path, not as a catch-all. Move it inside `handle { ... }` (unnamed catch-all) at the end of the block.",
			opener)}
	}
	return nil
}

var (
	rateLimitDirRe  = regexp.MustCompile(`(?m)^\s*rate_limit\s*\{`)
	remoteHostKeyRe = regexp.MustCompile(`(?m)^\s*key\s*\{remote_host\}`)
	cfKeyRe         = regexp.MustCompile(`key\s*\{http\.request\.header\.cf-connecting-ip\}`)
	registerRouteRe = regexp.MustCompile(`(?m)^\s*handle\s+/self-service/registration/methods/password\s*\{`)
	// registerRouteTargetRe pins the destination of the registration route:
	// the loopruntime-cloud BFF, which unifies the response and so closes account
	// enumeration. The port is only the BFF's address; the invariant is that
	// registration reaches the BFF and never Kratos directly.
	registerRouteTargetRe = regexp.MustCompile(`(?ms)handle\s+/self-service/registration/methods/password\s*\{[^}]*reverse_proxy\s+localhost:3002`)
)

// checkAuthDevBlock audits the auth-dev host block (plan
// kratos-self-service-rate-limit-systemic): the rate_limit zones, their
// per-real-IP key, and the registration route through the BFF.
func checkAuthDevBlock(content string) []string {
	block, ok := extractAuthDevBlock(content)
	if !ok {
		return []string{"assert_auth_dev_block_present — `auth-dev.getloopscale.com { ... }` host block MISSING. Plan kratos-self-service-rate-limit-systemic T1.2 declares 4 rate_limit zones MUST live inside this block."}
	}
	violations := checkRateLimit(block)
	return append(violations, checkRegisterRoute(block)...)
}

// checkRateLimit asserts the rate_limit container holds the four canonical
// zones, each keyed on cf-connecting-ip. {remote_host} resolves to the CF
// tunnel's hop, so every CF-fronted user would share one bucket.
func checkRateLimit(block string) []string {
	if !rateLimitDirRe.MatchString(block) {
		return []string{"assert_authdev_rate_limit_block_present — `rate_limit { ... }` directive MISSING in auth-dev host block. Red Team loopruntime-cloud#68 login + #89 recovery email-bomb stay open without it (per blueprint Q1 canonical zones)."}
	}
	var violations []string
	for _, zone := range canonicalRateLimitZones {
		if !strings.Contains(block, zone) {
			violations = append(violations, fmt.Sprintf(
				"assert_authdev_rate_limit_zone_%s — `%s` MISSING in auth-dev host block. All 4 canonical zones (login + register + recovery submit + recovery flow-create) are required per plan kratos-self-service-rate-limit-systemic T1.2.",
				strings.TrimPrefix(zone, "zone "), zone))
		}
	}
	if remoteHostKeyRe.MatchString(block) {
		violations = append(violations,
			"assert_authdev_rate_limit_key_cf_connecting_ip — `key {remote_host}` found inside auth-dev rate_limit block. EC-2 absorption: must use `key {http.request.header.cf-connecting-ip}` so per-IP buckets reflect the real end-user IP through Cloudflare Tunnel (otherwise the bucket shares one slot across all CF-fronted users = FAANG-grade DoS).")
	}
	if !cfKeyRe.MatchString(block) {
		violations = append(violations,
			"assert_authdev_rate_limit_key_cf_connecting_ip — no `key {http.request.header.cf-connecting-ip}` found inside rate_limit zones. EC-2 absorption: required for per-real-IP buckets through CF Tunnel.")
	}
	return violations
}

// checkRegisterRoute asserts registration is routed through the loopruntime-cloud
// BFF (Red Team loopruntime-cloud#43).
func checkRegisterRoute(block string) []string {
	if !registerRouteRe.MatchString(block) {
		return []string{"assert_register_route_proxied_to_cloud — `handle /self-service/registration/methods/password { ... }` MISSING in auth-dev host block. Red Team loopruntime-cloud#43 closure (BFF response-shape unifier per T3.1) is unreachable without this routing handle (T3.2)."}
	}
	if !registerRouteTargetRe.MatchString(block) {
		return []string{"assert_register_route_proxied_to_cloud — `handle /self-service/registration/methods/password` does NOT reverse_proxy to localhost:3002 (loopruntime-cloud BFF). Red Team loopruntime-cloud#43 closure requires this exact target."}
	}
	return nil
}

var (
	// leadingBraceRe matches the first `{` of a block that opens after whitespace.
	leadingBraceRe         = regexp.MustCompile(`^\s*\{`)
	trustedProxiesStaticRe = regexp.MustCompile(`(?m)trusted_proxies\s+static\b`)
	trustedProxiesRe       = regexp.MustCompile(`(?m)^\s*trusted_proxies\s+static\s`)
	ipv4CIDRRe             = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}/\d{1,2}\b`)
	ipv6CIDRRe             = regexp.MustCompile(`\b[0-9a-fA-F:]+::?(?:[0-9a-fA-F]{1,4})?/\d{1,3}\b`)
)

// Minimum CF ranges pinned in trusted_proxies. CF publishes ~15 IPv4 and 7
// IPv6 ranges; either family missing re-opens the bucket-collision bypass.
const (
	minTrustedIPv4CIDRs = 10
	minTrustedIPv6CIDRs = 5
)

// checkTrustedProxies asserts the global `servers { trusted_proxies static }`
// pins CF's ranges, so Caddy honours cf-connecting-ip only from CF hops. CIDRs
// are counted inside that block only: counting file-wide would let CIDRs in
// comments satisfy the gate while trusted_proxies stays empty.
func checkTrustedProxies(content string) []string {
	if !trustedProxiesRe.MatchString(content) {
		return []string{"assert_trusted_proxies_static_cf_ranges — global `servers { trusted_proxies static ... }` block MISSING from Caddyfile. EC-2 absorption: without trusted_proxies CF ranges, Caddy refuses to honor cf-connecting-ip header → rate_limit buckets share one slot across all CF traffic. Pin CF's published IPv4 ranges per https://www.cloudflare.com/ips/ + annual refresh."}
	}
	body, ok := extractTrustedProxiesBlock(content)
	if !ok {
		return []string{"assert_trusted_proxies_block_extractable — `trusted_proxies static { ... }` body brace-balance failed. Block likely truncated or malformed."}
	}
	var violations []string
	if n := len(ipv4CIDRRe.FindAllString(body, -1)); n < minTrustedIPv4CIDRs {
		violations = append(violations, fmt.Sprintf(
			"assert_trusted_proxies_cf_ipv4_count — only %d IPv4 CIDR(s) inside trusted_proxies block; CF publishes ~15. Plan kratos-self-service-rate-limit-systemic T1.2 requires ≥ 10 CF IPv4 CIDRs pinned.",
			n))
	}
	if n := len(ipv6CIDRRe.FindAllString(body, -1)); n < minTrustedIPv6CIDRs {
		violations = append(violations, fmt.Sprintf(
			"assert_trusted_proxies_cf_ipv6_count — only %d IPv6 CIDR(s) inside trusted_proxies block; CF publishes 7. F-sec-4 absorption: pin all CF IPv6 ranges to close the IPv6-hop bypass.",
			n))
	}
	return violations
}

// extractTrustedProxiesBlock returns the body inside `trusted_proxies static {
// ... }` OR — when the static form is used inline as `trusted_proxies static
// 1.2.3.4/16 5.6.7.0/24` (no braces, line continuations via `\`) — returns
// the contiguous line-continuation tail. F-arch-4 absorption: counting CIDRs
// only inside this block defeats the bypass where a Caddyfile sprinkles CIDR
// strings in comments while keeping trusted_proxies empty. Returns (body,
// true) on success; ("", false) when no `trusted_proxies static` is present.
func extractTrustedProxiesBlock(content string) (string, bool) {
	// Two canonical shapes:
	//   (1) `trusted_proxies static <ipv4cidr> <ipv6cidr> ...` over one or more
	//       lines (line-continuation `\`).
	//   (2) `trusted_proxies static { ... }` block form (less common).
	loc := trustedProxiesStaticRe.FindStringIndex(content)
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

// extractAuthDevBlock returns the body of the `auth-dev.getloopscale.com { ... }`
// host block via brace-balance counting starting at the opening `{`. T1.3 of
// plan kratos-self-service-rate-limit-systemic v1.1. Returns (body, true) on
// success, ("", false) when the opening directive is missing or unbalanced.
func extractAuthDevBlock(content string) (string, bool) {
	return qgshell.ExtractBraceBlock(content, authDevBlockOpenRe)
}

// extractAppDevBlock returns the body of the `app-dev.getloopscale.com { ... }`
// block via brace-balance counting starting at the opening `{`. Returns
// (body, true) on success, ("", false) when the opening directive is missing
// or the braces are unbalanced.
func extractAppDevBlock(content string) (string, bool) {
	return qgshell.ExtractBraceBlock(content, blockOpenRe)
}
