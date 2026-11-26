// Package caddyfilenodockerip enforces that the canonical infra/local/Caddyfile
// does NOT hardcode Docker network IPs (172.18.x.x) in non-comment lines and
// that both `*.prod-dev.getloopscale.app` + `*.preview-dev.getloopscale.app` wildcard
// blocks reach the cells only through stable host ports — the single upstream
// `https://127.0.0.1:8443`, or a local snippet mapping every cell to a
// 127.0.0.1 port (per-cell routing, #447).
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

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qgshell"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
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
	prodDevHeader    = regexp.MustCompile(`(?m)^\*\.prod-dev\.getloopscale\.app\s*\{`)
	previewDevHeader = regexp.MustCompile(`(?m)^\*\.preview-dev\.getloopscale\.app\s*\{`)
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

// ErrViolations means at least one invariant failed.
var ErrViolations = errors.New("Caddyfile hardcoded 172.18.x.x IP OR missing canonical 127.0.0.1:8443 upstream")

// Check reads infra/local/Caddyfile and asserts three invariants:
//  1. no non-comment line contains a 172.18.x.x literal;
//  2. the *.prod-dev.getloopscale.app block reaches the cells over loopback host ports;
//  3. the *.preview-dev.getloopscale.app block reaches the cells over loopback host ports.
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
	if !hasCanonicalUpstream(string(body), prodDevHeader) {
		violations = append(violations, fmt.Sprintf("%s: *.prod-dev.getloopscale.app block missing canonical `%s`", caddyfilePath, canonicalUpstream))
	}
	if !hasCanonicalUpstream(string(body), previewDevHeader) {
		violations = append(violations, fmt.Sprintf("%s: *.preview-dev.getloopscale.app block missing canonical `%s`", caddyfilePath, canonicalUpstream))
	}

	if len(violations) > 0 {
		return Outcome{Violations: violations, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

var (
	importLineRE  = regexp.MustCompile(`(?m)^\s*import\s+(\S+)\s*$`)
	mapHeaderRE   = regexp.MustCompile(`(?m)^\s*map\s+\S+\s+(\{[^}\s]+\})\s*\{`)
	loopbackDest  = regexp.MustCompile(`^127\.0\.0\.1:[0-9]+$`)
	cellZeroEntry = "127.0.0.1:8443"
)

// hasCanonicalUpstream reports whether the block opened by header reaches the cells
// only through stable host ports: either it proxies to canonicalUpstream itself, or
// it imports a local snippet whose cell map sends every cell — cell-0 on 8443
// included — to a 127.0.0.1 port and proxies to that map's output. Per-cell routing
// (#447) keeps the #127 property: no destination can drift with Docker's DHCP.
func hasCanonicalUpstream(content string, header *regexp.Regexp) bool {
	body, ok := qgshell.ExtractBraceBlock(content, header)
	if !ok {
		return false
	}
	if strings.Contains(body, canonicalUpstream) {
		return true
	}
	for _, m := range importLineRE.FindAllStringSubmatch(body, -1) {
		snippetHeader := regexp.MustCompile(`(?m)^\(` + regexp.QuoteMeta(m[1]) + `\)\s*\{`)
		if snippet, found := qgshell.ExtractBraceBlock(content, snippetHeader); found && routesCellsOverLoopback(snippet) {
			return true
		}
	}
	return false
}

// routesCellsOverLoopback reports whether snippet maps cells to loopback ports only,
// maps cell-0's ingress, and proxies to the map's output placeholder.
func routesCellsOverLoopback(snippet string) bool {
	m := mapHeaderRE.FindStringSubmatchIndex(snippet)
	if m == nil {
		return false
	}
	placeholder := snippet[m[2]:m[3]]
	entries, ok := qgshell.ExtractBraceBlock(snippet[m[0]:], mapHeaderRE)
	if !ok || !strings.Contains(snippet, "reverse_proxy "+placeholder) {
		return false
	}
	sawCellZero := false
	for _, line := range strings.Split(entries, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		dest := fields[len(fields)-1]
		if len(fields) != 2 || !loopbackDest.MatchString(dest) {
			return false
		}
		sawCellZero = sawCellZero || dest == cellZeroEntry
	}
	return sawCellZero
}
