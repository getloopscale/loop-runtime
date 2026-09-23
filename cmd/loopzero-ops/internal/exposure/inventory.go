// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package exposure owns the format of infra/quality/proxy-exposure.txt, the
// declaration of every route the public edge proxies.
//
// Two consumers read it: the hermetic proxy-exposure gate (every Caddyfile
// reverse_proxy must be declared) and the live leak probe (every declared
// route is probed). They used to carry three parsers with different line
// rules, so a format change had to be made three times or the probe and the
// gate would disagree about what is declared.
package exposure

import (
	"fmt"
	"os"
	"strings"
)

// Declaration classes: what the route is allowed to expose.
const (
	// ClassFiltered — the edge restricts what reaches the client.
	ClassFiltered = "filtered"
	// ClassInternal — the upstream returns no user data at all.
	ClassInternal = "internal"
	// ClassReviewed — full passthrough, looked at and accepted.
	ClassReviewed = "reviewed"
)

var validClasses = map[string]bool{ClassFiltered: true, ClassInternal: true, ClassReviewed: true}

// Declaration is one row: `<site> <path> | <class> | <rationale>`, where
// <path> is `-` for a site-level proxy.
type Declaration struct {
	Site      string
	Path      string
	Class     string
	Rationale string
	Line      int
}

// Key is "<site> <path>", the form the gate matches Caddyfile proxies against.
func (d Declaration) Key() string { return d.Site + " " + d.Path }

// Parse reads the inventory text. Blank lines and `#` comments are skipped;
// every other line must be a well-formed declaration, and one that is not is
// reported in malformed (with its line number) rather than guessed at.
func Parse(content string) (decls []Declaration, malformed []string) {
	for i, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		d, err := parseLine(t)
		if err != nil {
			malformed = append(malformed, fmt.Sprintf("line %d: %v", i+1, err))
			continue
		}
		d.Line = i + 1
		decls = append(decls, d)
	}
	return decls, malformed
}

func parseLine(t string) (Declaration, error) {
	parts := strings.Split(t, "|")
	if len(parts) != 3 {
		return Declaration{}, fmt.Errorf("want `<site> <path> | <class> | <rationale>`, got %q", t)
	}
	route := strings.Fields(parts[0])
	if len(route) != 2 {
		return Declaration{}, fmt.Errorf("want `<site> <path>` before the first `|` (use `-` for a site-level proxy), got %q", strings.TrimSpace(parts[0]))
	}
	class := strings.TrimSpace(parts[1])
	if !validClasses[class] {
		return Declaration{}, fmt.Errorf("unknown class %q (want filtered|internal|reviewed)", class)
	}
	rationale := strings.TrimSpace(parts[2])
	if rationale == "" {
		return Declaration{}, fmt.Errorf("empty rationale — a class without a reason is an oversight with a label")
	}
	return Declaration{Site: route[0], Path: route[1], Class: class, Rationale: rationale}, nil
}

// Load reads and parses the inventory at path. A missing file is an error:
// zero declarations is indistinguishable from "nothing is exposed".
func Load(path string) ([]Declaration, []string, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- operator/CI-supplied path to the versioned inventory
	if err != nil {
		return nil, nil, fmt.Errorf("read exposure inventory %s: %w", path, err)
	}
	decls, malformed := Parse(string(b))
	return decls, malformed, nil
}
