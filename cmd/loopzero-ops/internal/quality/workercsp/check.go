// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package workercsp guards the Content-Security-Policy the Cloudflare static
// worker attaches to every response it serves (issue #235).
//
// The worker fronts tenant static content on *.dev.loopzero.example, so its CSP is
// the last header a tenant's users receive. `script-src 'unsafe-inline'` there
// negates most of what the CSP would buy: any injected inline <script> in the
// served HTML executes, and the blast radius is every tenant's static surface.
//
// WHY A SEPARATE GATE from `caddyfile-csp`: that one asserts invariants of the
// operator-local Caddy edge, a different proxy on a different layer. The two
// share a topic, not a subject — folding this into it would make a failure
// there ambiguous about which edge is at fault.
//
// SCOPE — deliberately narrow. This asserts the ABSENCE of the two directives
// that let arbitrary script run. It does NOT try to validate the CSP as a
// whole: a full policy parser would encode opinions (which hosts are
// acceptable in connect-src?) that belong in review, not in a gate. A gate
// that fails on debatable grounds gets exemptions written for it until it
// means nothing.
//
// `style-src 'unsafe-inline'` is deliberately NOT failed here. It is weaker
// hygiene, but inline styles do not execute code, and the issue itself scopes
// it as a later step. Conflating the two would let the urgent one be deferred
// alongside the cosmetic one.
package workercsp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultWorkerPath is the Cloudflare static-delivery worker source.
const DefaultWorkerPath = "infra/cloudflare/worker.mjs"

// Config tells Check where to look.
type Config struct {
	RepoRoot   string
	WorkerPath string // relative to RepoRoot; defaults to DefaultWorkerPath
}

// Finding is one violation, with the line so an operator can go straight to it.
type Finding struct {
	Line      int
	Directive string // e.g. "script-src"
	Detail    string
}

// Outcome captures the gate result.
type Outcome struct {
	Pass     bool
	Skipped  bool
	Reason   string
	Findings []Finding
}

// cspLineRe finds the CSP header assignment regardless of quoting style.
var cspLineRe = regexp.MustCompile(`(?i)content-security-policy`)

// scriptSrcRe captures the script-src directive body up to the next `;` or the
// closing double quote of the JS string literal.
//
// The body deliberately ALLOWS single quotes: CSP keyword values are written
// `'self'`, `'unsafe-inline'`, `'nonce-…'`. An earlier version excluded `'`
// from the body and therefore captured an empty string for every real policy
// — the gate reported PASS against a worker that plainly permitted
// unsafe-inline. The table tests caught it; the live check alone would not
// have, because "no findings" and "matched nothing" look identical from
// outside. Anchored on the directive name so `connect-src` carrying the same
// literal cannot be mistaken for it, and stopping at `;` so a later
// directive's value is never attributed here.
var scriptSrcRe = regexp.MustCompile(`(?i)script-src([^;"]*)`)

// Check asserts the worker CSP does not permit arbitrary inline or eval'd script.
func Check(cfg Config) (*Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	rel := cfg.WorkerPath
	if rel == "" {
		rel = DefaultWorkerPath
	}
	path := filepath.Join(cfg.RepoRoot, rel)

	body, err := os.ReadFile(path) //nolint:gosec // operator-controlled repo path
	if err != nil {
		// A missing worker is not a silent pass: say so, so nobody reads the
		// absence of findings as proof the policy is sound.
		return &Outcome{
			Skipped: true,
			Reason:  fmt.Sprintf("worker source not found at %s (%v) — CSP NOT verified", rel, err),
		}, nil
	}

	out := &Outcome{Pass: true}
	for i, line := range strings.Split(string(body), "\n") {
		if !cspLineRe.MatchString(line) {
			continue
		}
		m := scriptSrcRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		directiveBody := m[1]
		for _, bad := range []string{"'unsafe-inline'", "'unsafe-eval'"} {
			if strings.Contains(directiveBody, bad) {
				out.Pass = false
				out.Findings = append(out.Findings, Finding{
					Line:      i + 1,
					Directive: "script-src",
					Detail: fmt.Sprintf("script-src permits %s — any injected inline script in served tenant HTML executes, "+
						"which negates most of the XSS protection this CSP exists to provide", bad),
				})
			}
		}
	}
	if out.Pass {
		out.Reason = fmt.Sprintf("%s: script-src permits neither 'unsafe-inline' nor 'unsafe-eval'", rel)
	}
	return out, nil
}
