// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package proxyexposure implements the proxy-exposure gate: every
// `reverse_proxy` on the public edge must be declared, with the shape of what
// it lets through, in infra/quality/proxy-exposure.txt.
//
// Why a declaration and not a scan of the response: a reverse_proxy inherits
// its response contract from whatever sits behind it, so the risk is invisible
// in this repository. `handle /sessions/* { reverse_proxy <kratos> }` on
// app-dev.loopzero.example returns the whole Ory session — including
// devices[].ip_address and recovery_addresses, neither of which the dashboard
// reads (measured 2026-08-06 against loopzero-cloud/dashboard/src). Nothing in the
// Caddyfile says so, and nothing failed when it started being true.
//
// The gate cannot prove a response is safe — only a LIVE probe can, and that is
// what `task leak:live` is for. What it CAN do is make the surface complete:
// every proxy is classified by a human, and a new one cannot appear without
// somebody writing down what it exposes. That is the 100% this gate offers, and
// the distinction is deliberate — see the classes below.
package proxyexposure

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultCaddyfile is the edge config scanned when Config.Caddyfile is empty.
const DefaultCaddyfile = "infra/local/Caddyfile"

// DefaultRegistry is the declaration file.
const DefaultRegistry = "infra/quality/proxy-exposure.txt"

// Exposure classes. A declaration carries exactly one.
const (
	// ClassFiltered — the edge restricts what reaches the client (a field
	// allowlist, an explicit sub-path, a rewritten body).
	ClassFiltered = "filtered"
	// ClassInternal — the upstream returns no user data at all (static assets,
	// websockets, health). Nothing to leak.
	ClassInternal = "internal"
	// ClassReviewed — full passthrough, looked at, and accepted. The rationale
	// column is what makes this different from an oversight.
	ClassReviewed = "reviewed"
)

var validClasses = map[string]bool{
	ClassFiltered: true, ClassInternal: true, ClassReviewed: true,
}

var (
	blockOpenRe   = regexp.MustCompile(`^(.*?)\{\s*$`)
	reverseProxRe = regexp.MustCompile(`^\s*reverse_proxy\b`)
	commentRe     = regexp.MustCompile(`^\s*#`)
)

// Exposure is one reverse_proxy found on the edge.
type Exposure struct {
	Site string // e.g. app-dev.loopzero.example
	Path string // e.g. /sessions/*  ("" when the proxy is site-level)
	Line int
}

// Key is the registry key: "<site> <path>", with "-" standing in for a
// site-level proxy that has no enclosing handle.
func (e Exposure) Key() string {
	p := e.Path
	if p == "" {
		p = "-"
	}
	return e.Site + " " + p
}

// Declaration is one parsed registry row.
type Declaration struct {
	Key       string
	Class     string
	Rationale string
}

// Config drives the gate.
type Config struct {
	Root      string
	Caddyfile string
	Registry  string
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Scanned    int      // reverse_proxy directives found
	Undeclared []string // exposures with no registry row
	Stale      []string // registry rows matching no exposure
	Malformed  []string // registry rows that do not parse
}

// Check runs the gate.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	caddyPath := cfg.Caddyfile
	if caddyPath == "" {
		caddyPath = filepath.Join(root, DefaultCaddyfile)
	}
	regPath := cfg.Registry
	if regPath == "" {
		regPath = filepath.Join(root, DefaultRegistry)
	}

	data, err := os.ReadFile(caddyPath)
	if err != nil {
		return nil, fmt.Errorf("read Caddyfile at %s: %w", caddyPath, err)
	}
	exposures := parseExposures(string(data))

	// Fail closed on an empty scan. A Caddyfile with no reverse_proxy means the
	// parser broke or the path is wrong, not that the edge proxies nothing —
	// and "0 scanned, PASS" is a green verdict asserting nothing (issue #255).
	if len(exposures) == 0 {
		return &Outcome{
			Undeclared: []string{"no reverse_proxy found in " + caddyPath + " — parser or path is wrong; refusing to pass vacuously"},
		}, nil
	}

	regData, err := os.ReadFile(regPath)
	if err != nil {
		return nil, fmt.Errorf("read registry at %s: %w", regPath, err)
	}
	decls, malformed := parseRegistry(string(regData))

	out := &Outcome{Pass: true, Scanned: len(exposures), Malformed: malformed}

	seen := map[string]bool{}
	for _, e := range exposures {
		k := e.Key()
		seen[k] = true
		if _, ok := decls[k]; !ok {
			out.Undeclared = append(out.Undeclared,
				fmt.Sprintf("%s (Caddyfile:%d) — no row in %s", k, e.Line, DefaultRegistry))
		}
	}
	// A row that matches nothing is a decision about an edge that no longer
	// exists; leaving it in makes the registry read as covering more than it does.
	for k := range decls {
		if !seen[k] {
			out.Stale = append(out.Stale, k)
		}
	}

	sort.Strings(out.Undeclared)
	sort.Strings(out.Stale)
	if len(out.Undeclared) > 0 || len(out.Stale) > 0 || len(out.Malformed) > 0 {
		out.Pass = false
	}
	return out, nil
}

// parseExposures walks the Caddyfile tracking the brace stack, so each
// reverse_proxy is attributed to its site and, when present, its handle path.
func parseExposures(src string) []Exposure {
	var stack []string
	var out []Exposure

	for i, raw := range strings.Split(src, "\n") {
		line := raw
		if commentRe.MatchString(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if reverseProxRe.MatchString(line) {
			site, path := attribute(stack)
			if site != "" {
				out = append(out, Exposure{Site: site, Path: path, Line: i + 1})
			}
			// fallthrough: a reverse_proxy line may itself open a block
		}

		if m := blockOpenRe.FindStringSubmatch(line); m != nil {
			stack = append(stack, strings.TrimSpace(m[1]))
			continue
		}
		if strings.HasPrefix(trimmed, "}") && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	return out
}

// attribute reads the site (outermost frame that looks like a hostname) and the
// nearest enclosing handle path off the brace stack.
func attribute(stack []string) (site, path string) {
	for _, frame := range stack {
		head := strings.Fields(frame)
		if len(head) == 0 {
			continue
		}
		switch {
		case site == "" && strings.Contains(head[0], "."):
			site = head[0]
		case head[0] == "handle" || head[0] == "handle_path":
			if len(head) > 1 {
				path = head[1]
			} else {
				path = "/*" // bare `handle {` — the catch-all
			}
		}
	}
	return site, path
}

// parseRegistry returns key -> declaration, plus the rows that do not parse.
// Format: <site> <path> | <class> | <rationale>
func parseRegistry(content string) (map[string]Declaration, []string) {
	decls := map[string]Declaration{}
	var malformed []string
	for i, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		parts := strings.Split(t, "|")
		if len(parts) != 3 {
			malformed = append(malformed, fmt.Sprintf("line %d: want `<site> <path> | <class> | <rationale>`, got %q", i+1, t))
			continue
		}
		key := strings.Join(strings.Fields(parts[0]), " ")
		class := strings.TrimSpace(parts[1])
		rationale := strings.TrimSpace(parts[2])
		if !validClasses[class] {
			malformed = append(malformed, fmt.Sprintf("line %d: unknown class %q (want filtered|internal|reviewed)", i+1, class))
			continue
		}
		if rationale == "" {
			malformed = append(malformed, fmt.Sprintf("line %d: empty rationale — a class without a reason is an oversight with a label", i+1))
			continue
		}
		decls[key] = Declaration{Key: key, Class: class, Rationale: rationale}
	}
	return decls, malformed
}
