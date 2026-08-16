// Package caddyfilenodockerip enforces that the canonical infra/local/Caddyfile
// does NOT hardcode Docker network IPs (172.18.x.x) in non-comment lines and
// that both `*.prod-dev.loopzero.example` + `*.preview-dev.loopzero.example` wildcard
// blocks reverse_proxy to the stable host-port form `https://127.0.0.1:8443`.
//
// Root cause of Red Team loopzero-cloud#127: the canonical Caddyfile reverse_proxy
// upstream was hardcoded to `https://172.18.0.10:443` — a Docker network IP
// assigned by DHCP that drifted from k3d-loopzero-cell-0-serverlb to
// k3d-loopzero-secrets-serverlb after `docker restart`. Every customer-app request
// then routed to the VAULT cluster → 502 sustentado for the ENTIRE dev-public
// probe surface. The stable host-port form 127.0.0.1:8443 does NOT drift.
//
// Migrated from infra/tests/check-caddyfile-no-docker-network-ip_test.sh
// (M-Migrate2 continuation, 2026-07-07).
// Hermetic: NO network, NO docker, NO caddy — pure text walk over Caddyfile.
package caddyfilenodockerip

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// caddyfilePath is the canonical dev-public edge config, relative to repo root.
const caddyfilePath = "infra/local/Caddyfile"

// dockerNetIPPattern matches the 172.18.x.x subnet Docker uses for the default
// bridge in k3d — the exact IP class the Red Team #127 fix banned.
var dockerNetIPPattern = regexp.MustCompile(`172\.18\.[0-9]+\.[0-9]+`)

// prodDevHeader / previewDevHeader mark the start of the two wildcard blocks
// the fix pattern MUST honor. The `{` at end must be present so a stray
// comment reference to the hostname elsewhere is not mistaken for the block.
var (
	prodDevHeader    = regexp.MustCompile(`^\*\.prod-dev\.loopzero\.dev\s*\{`)
	previewDevHeader = regexp.MustCompile(`^\*\.preview-dev\.loopzero\.dev\s*\{`)
)

// canonicalUpstream is the stable host-port form the two wildcard blocks
// MUST reverse_proxy to (parallel to Caddyfile lines 594 + 604 fix).
const canonicalUpstream = "reverse_proxy https://127.0.0.1:8443"

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary the CLI prints.
type Outcome struct {
	Violations []string
}

// ErrViolations means at least one invariant failed.
var ErrViolations = errors.New("Caddyfile hardcoded 172.18.x.x IP OR missing canonical 127.0.0.1:8443 upstream")

// Check reads infra/local/Caddyfile and asserts three invariants:
//  1. no non-comment line contains a 172.18.x.x literal;
//  2. the *.prod-dev.loopzero.example block reverse_proxies to https://127.0.0.1:8443;
//  3. the *.preview-dev.loopzero.example block reverse_proxies to https://127.0.0.1:8443.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, caddyfilePath)
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{}, fmt.Errorf("read Caddyfile at %s: %w", full, err)
	}
	lines := strings.Split(string(body), "\n")

	var violations []string

	// Invariant 1: zero non-comment lines contain 172.18.x.x.
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if dockerNetIPPattern.MatchString(line) {
			violations = append(violations, fmt.Sprintf("%s:%d: docker network IP hardcoded: %s", caddyfilePath, i+1, strings.TrimSpace(line)))
		}
	}

	// Invariants 2+3: each wildcard block reverse_proxies to the canonical stable upstream.
	if !hasCanonicalUpstream(lines, prodDevHeader) {
		violations = append(violations, fmt.Sprintf("%s: *.prod-dev.loopzero.example block missing canonical `%s`", caddyfilePath, canonicalUpstream))
	}
	if !hasCanonicalUpstream(lines, previewDevHeader) {
		violations = append(violations, fmt.Sprintf("%s: *.preview-dev.loopzero.example block missing canonical `%s`", caddyfilePath, canonicalUpstream))
	}

	if len(violations) > 0 {
		return Outcome{Violations: violations}, ErrViolations
	}
	return Outcome{}, nil
}

// hasCanonicalUpstream walks lines from the first match of header until the
// matching `}` and returns true if any line in that block contains
// canonicalUpstream. Balances nested `{`/`}` so inner sub-directives with their
// own braces do not close the outer block early.
func hasCanonicalUpstream(lines []string, header *regexp.Regexp) bool {
	inBlock := false
	depth := 0
	for _, line := range lines {
		if !inBlock {
			if header.MatchString(line) {
				inBlock = true
				depth = strings.Count(line, "{") - strings.Count(line, "}")
			}
			continue
		}
		if strings.Contains(line, canonicalUpstream) {
			return true
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth <= 0 {
			return false
		}
	}
	return false
}
