// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package caddyhostparity refuses a hostname that the edge verifier demands a certificate
// for and the Caddyfile never declares a site block for.
//
// # The defect, measured
//
// On 2026-08-18, the GitHub App delivery log (id 3319955) showed **every** delivery
// failing the same way, including the `push` of a real commit:
//
//	POST https://cloud-dev.loopzero.example/api/v1/webhooks/github
//	giving up after 1 attempt(s): remote error: tls: internal error   code=500
//
// The first reading blamed the `Caddyfile` for not declaring a block for that hostname. It
// was backwards, and the review caught it: the `Caddyfile` **removed** `cloud-dev`
// deliberately on 2026-08-10, with the reason written six lines below the last block — it
// was a public host serving a redirect, with an auth surface granted to nobody, and the
// webhook payload already lived on `app-dev`, which has the `handle /api/v1/webhooks/github`.
//
// The stale side was `infra/scripts/local/caddy-up-dev-public.sh`, which kept demanding a
// certificate for a hostname nobody serves any more. As a result, `--verify` failed 100% of
// the time against the correct configuration — and a verifier that cannot pass is a
// verifier nobody runs. That is how weeks of failed deliveries never became a signal, and
// it is the divergence, not the absence, that this gate measures.
//
// # What this gate verifies
//
// That every hostname demanded by the edge verifier has, in the `Caddyfile`, a site address
// that serves it. It is hermetic — it compares two declarations in the repository, with no
// network — so it runs on every delivery without depending on an issued certificate.
//
// # The limit, stated out loud
//
// It compares declarations. It does not prove the certificate was issued, that DNS
// resolves, or that the destination behind the address answers. A block pointing at the
// wrong port passes here and fails live. When the `Caddyfile` imports anything but a
// snippet it defines itself, the view is partial and the gate refuses instead of
// reporting parity over half the file.
package caddyhostparity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scanresult"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// ErrUnserved means a demanded hostname has no site block to terminate its TLS.
var ErrUnserved = errors.New("hostname demanded by the verifier with no site block")

// ErrVerifierMissing means the script that prescribes the host list is absent.
//
// With no prescription there is no parity to verify, and reporting "no divergence" would be
// green over nothing.
var ErrVerifierMissing = errors.New("edge verifier missing — with no demanded list there is no parity")

// ErrCaddyfileMissing means the served-hosts side of the comparison is absent.
var ErrCaddyfileMissing = errors.New("Caddyfile missing — with no site blocks there is nothing to compare")

// ErrPartialView means the Caddyfile pulls site blocks this gate cannot see.
//
// Reporting parity over half the file would be green over what was not read.
var ErrPartialView = errors.New("the Caddyfile imports another file — parity is unverifiable over a partial view")

// ErrNothingScanned means the host list parsed empty.
//
// Zero demanded is indistinguishable from a broken parse, and it is exactly the
// `PASS — 0 hostnames` shape this ecosystem has already paid to learn.
var ErrNothingScanned = fmt.Errorf("%w: no demanded hostname", scanresult.ErrNothingScanned)

const (
	verifierRel  = "infra/scripts/local/caddy-up-dev-public.sh"
	caddyfileRel = "infra/local/Caddyfile"
)

var (
	// hostsArrayRE isolates the body of the verifier's `hosts=( ... )` array. Anchored at
	// line start so it does not capture another array before it, and closing only on a `)`
	// that opens a line — a `)` in the body (from `$(...)`, from a comment) silently
	// truncated the list, and "1 of 6 demanded" reported the same as "6 demanded".
	hostsArrayRE = regexp.MustCompile(`(?m)^\s*(?:local\s+)?hosts=\(\s*$([\s\S]*?)^\s*\)`)
	// hostRE accepts a hostname, with or without a leading wildcard label.
	hostRE = regexp.MustCompile(`^(\*\.)?[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	// siteAddrLineRE captures the whole address line of a site block: column zero up to
	// the opening brace, on the SAME line. The column anchor is what separates a served
	// site from a mention inside a directive.
	siteAddrLineRE = regexp.MustCompile(`(?m)^([^\s{][^{\n]*?)[ \t]*\{[ \t]*$`)
	// importRE finds imports. One naming a snippet defined in this file adds no site
	// block and is local; any other import pulls blocks this gate cannot see.
	importRE  = regexp.MustCompile(`(?m)^\s*import\s+(\S+)`)
	snippetRE = regexp.MustCompile(`(?m)^\(([^)\s]+)\)[ \t]*\{`)
)

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Finding is one demanded hostname with nothing to terminate its TLS.
type Finding struct {
	Host       string
	DemandedBy string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned  int
	Served   int
	Findings []Finding
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// serves reports whether a declared site address covers the hostname, honouring the
// wildcard form: `*.preview-dev.loopzero.example` serves every single label beneath it, and
// neither a deeper host nor the apex.
func serves(block, host string) bool {
	// Caddy serves everything behind a catch-all; treating it as unserved would fail a
	// configuration that works.
	if block == "*" || strings.HasPrefix(block, ":") {
		return true
	}
	if block == host {
		return true
	}
	suffix, isWildcard := strings.CutPrefix(block, "*.")
	if !isWildcard {
		return false
	}
	rest, ok := strings.CutSuffix(host, "."+suffix)
	// One label only: the Caddy wildcard does not cross a dot.
	return ok && rest != "" && !strings.Contains(rest, ".")
}

// demandedHosts parses the hostnames the edge verifier requires a certificate for.
func demandedHosts(root string) ([]string, error) {
	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, verifierRel)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrVerifierMissing, verifierRel)
	}
	m := hostsArrayRE.FindStringSubmatch(string(body))
	if m == nil {
		return nil, fmt.Errorf("%w: %s does not declare `hosts=(...)` in a recognisable form", ErrVerifierMissing, verifierRel)
	}
	var hosts []string
	seen := map[string]bool{}
	for _, f := range strings.Fields(m[1]) {
		f = strings.Trim(f, `"'`)
		if hostRE.MatchString(f) && !seen[f] {
			seen[f] = true
			hosts = append(hosts, f)
		}
	}
	return hosts, nil
}

// servedBlocks parses the site addresses the Caddyfile declares.
func servedBlocks(root string) ([]string, error) {
	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, caddyfileRel)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrCaddyfileMissing, caddyfileRel)
	}
	snippets := map[string]bool{}
	for _, m := range snippetRE.FindAllStringSubmatch(string(body), -1) {
		snippets[m[1]] = true
	}
	for _, m := range importRE.FindAllStringSubmatch(string(body), -1) {
		if !snippets[m[1]] {
			return nil, fmt.Errorf("%w: %s", ErrPartialView, caddyfileRel)
		}
	}
	var blocks []string
	for _, m := range siteAddrLineRE.FindAllStringSubmatch(string(body), -1) {
		if strings.HasPrefix(strings.TrimSpace(m[1]), "(") {
			continue // a snippet definition, not a site
		}
		// The comma is Caddy's canonical multi-host form; each token is an address.
		for _, tok := range strings.Split(m[1], ",") {
			if a := normalizeAddr(tok); a != "" {
				blocks = append(blocks, a)
			}
		}
	}
	return blocks, nil
}

// normalizeAddr reduces a Caddy site address to the hostname it serves, dropping the
// scheme and port and folding case — the Caddy address is case-insensitive, and a gate
// that is not would refuse a working configuration.
func normalizeAddr(tok string) string {
	a := strings.ToLower(strings.TrimSpace(tok))
	if i := strings.Index(a, "://"); i >= 0 {
		a = a[i+3:]
	}
	// A port with no host (`:443`) is a catch-all and survives whole; with a host, it is dropped.
	if i := strings.LastIndex(a, ":"); i > 0 {
		a = a[:i]
	}
	return strings.TrimSuffix(a, "/")
}

// Check compares every demanded hostname against the declared site addresses.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	hosts, err := demandedHosts(root)
	if err != nil {
		return out, err
	}
	if len(hosts) == 0 {
		return out, fmt.Errorf("%w: %s", ErrNothingScanned, verifierRel)
	}
	blocks, err := servedBlocks(root)
	if err != nil {
		return out, err
	}

	out.Scanned = len(hosts)
	for _, h := range hosts {
		covered := false
		for _, b := range blocks {
			if serves(b, h) {
				covered = true
				break
			}
		}
		if covered {
			out.Served++
			continue
		}
		out.Findings = append(out.Findings, Finding{Host: h, DemandedBy: verifierRel})
	}

	if len(out.Findings) > 0 {
		names := make([]string, 0, len(out.Findings))
		for _, f := range out.Findings {
			names = append(names, f.Host)
		}
		out.Violation = fmt.Errorf("%w: %s — demanded by %s, absent from %s",
			ErrUnserved, strings.Join(names, ", "), verifierRel, caddyfileRel)
		return out, nil
	}
	return out, nil
}
