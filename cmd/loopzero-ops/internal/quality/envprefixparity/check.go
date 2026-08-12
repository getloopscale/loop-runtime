// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package envprefixparity keeps the two declarations of the reserved
// environment-key prefixes from drifting apart.
//
// The rule is a security correction — build_plan.go:21-22 attributes it to
// "T2.5 … closes find_sec_004 MEDIUM" — and it exists twice, in two Go modules
// that cannot import each other: api/internal/services/deploycontracts (the
// control plane) and tools/loopzero-dockerfile-lint (a linter that runs inside the
// build container, and whose go.mod has zero requires). The only link between
// the copies is a prose comment saying "mirrors" — a convention nobody executes,
// and the copies had already diverged when this gate was written: the API side
// carried GOOGLE_ and the linter did not.
//
// WHAT THIS GATE PROVES, EXACTLY: that the two declared SETS OF PREFIXES are
// identical. Nothing else.
//
// That sentence is load-bearing, so it is spelled out rather than implied. A
// review of the two implementations (MEET-ASYNC-AMENDMENT-2026-08-11-001) found
// they diverge on THREE axes, and this gate covers one:
//
//	axis            api/internal/.../build_plan.go     tools/loopzero-dockerfile-lint
//	----            -----------------------------      --------------------------
//	prefix set      5 (incl. GOOGLE_)                  4                  <- COVERED HERE
//	severity        slog.Warn + continue (discarded    SeverityReject ->  <- B-069
//	                silently, deploy proceeds)         os.Exit(1)
//	case matching   case-SENSITIVE (k[:len(p)] == p)   case-INSENSITIVE   <- B-070
//	                                                   (strings.ToUpper)
//
// Equal sets are not an equal rule. A reader who sees this gate green must not
// conclude "the rule is unified" — two thirds of the divergence is out of its
// scope, tracked as B-069 and B-070. Saying so here is cheaper than a green
// gate that quietly overstates itself, which is the exact defect the slice this
// gate belongs to exists to remove.
package envprefixparity

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

// ErrPrefixSetsDiverge means the two declarations do not hold the same set.
var ErrPrefixSetsDiverge = errors.New("the two reservedEnvKeyPrefixes declarations hold different prefix sets")

// ErrDeclarationNotFound means a declaration could not be located or is not the
// expected shape. It is an error rather than a silent pass: a gate that cannot
// find its subject has not verified it, and reporting that as success is how
// `PASS — 0 routes` happened next door (B-040).
var ErrDeclarationNotFound = errors.New("could not locate a []string literal named reservedEnvKeyPrefixes")

// Declaration is one of the two copies.
type Declaration struct {
	File     string // repo-relative
	Symbol   string
	Prefixes []string
}

// Config is the gate input. The two paths are explicit rather than discovered:
// there are exactly two copies today (measured), and a discovery mechanism for
// a set of two would be the speculative generality rung 1 of the parsimony
// ladder refuses. If a third copy appears, it is added here — consciously.
type Config struct {
	RepoRoot string
	Sites    []Site
}

// Site names where a declaration is expected to live.
type Site struct {
	RelPath string
	Symbol  string
}

// DefaultSites are the two copies as of 2026-08-11.
func DefaultSites() []Site {
	return []Site{
		{RelPath: "api/internal/services/deploycontracts/build_plan.go", Symbol: "reservedEnvKeyPrefixes"},
		{RelPath: "tools/loopzero-dockerfile-lint/main.go", Symbol: "reservedEnvKeyPrefixes"},
	}
}

// Outcome reports what was compared.
type Outcome struct {
	Declarations []Declaration
	// OnlyIn maps a site's RelPath to the prefixes present only there.
	OnlyIn map[string][]string
}

// extractPrefixes parses a Go file and returns the string literals of the named
// `var <symbol> = []string{...}` declaration.
//
// It is AST-based, not a regexp over text, for the same reason servicesacyclic
// is: a string literal or a comment mentioning the symbol elsewhere in the file
// must not be mistaken for the declaration.
func extractPrefixes(absPath, symbol string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", absPath, err)
	}
	var found []string
	var ok bool
	ast.Inspect(f, func(n ast.Node) bool {
		if ok {
			return false
		}
		vs, isVS := n.(*ast.ValueSpec)
		if !isVS {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != symbol || i >= len(vs.Values) {
				continue
			}
			cl, isCL := vs.Values[i].(*ast.CompositeLit)
			if !isCL {
				continue
			}
			for _, el := range cl.Elts {
				bl, isBL := el.(*ast.BasicLit)
				if !isBL || bl.Kind != token.STRING {
					// A non-literal element (a constant, a call) means the
					// declaration is no longer a plain list, and comparing it
					// as one would compare something that is not there.
					return false
				}
				found = append(found, strings.Trim(bl.Value, `"`))
			}
			ok = true
			return false
		}
		return true
	})
	if !ok {
		return nil, fmt.Errorf("%w: %s in %s", ErrDeclarationNotFound, symbol, absPath)
	}
	return found, nil
}

// normalise turns a declaration into a comparable SET. Order is not part of the
// rule, and neither is repetition: two lists that reserve the same prefixes are
// the same rule however they are written.
func normalise(prefixes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Check compares the declared prefix sets across every configured site.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if len(cfg.Sites) == 0 {
		cfg.Sites = DefaultSites()
	}

	var out Outcome
	sets := map[string]map[string]bool{}
	union := map[string]bool{}

	for _, site := range cfg.Sites {
		prefixes, err := extractPrefixes(filepath.Join(cfg.RepoRoot, site.RelPath), site.Symbol)
		if err != nil {
			return out, err
		}
		norm := normalise(prefixes)
		out.Declarations = append(out.Declarations, Declaration{File: site.RelPath, Symbol: site.Symbol, Prefixes: norm})
		set := map[string]bool{}
		for _, p := range norm {
			set[p] = true
			union[p] = true
		}
		sets[site.RelPath] = set
	}

	out.OnlyIn = map[string][]string{}
	diverged := false
	for path, set := range sets {
		var only []string
		for p := range union {
			if !set[p] {
				continue
			}
			missingElsewhere := false
			for other, otherSet := range sets {
				if other != path && !otherSet[p] {
					missingElsewhere = true
				}
			}
			if missingElsewhere {
				only = append(only, p)
			}
		}
		if len(only) > 0 {
			sort.Strings(only)
			out.OnlyIn[path] = only
			diverged = true
		}
	}
	if diverged {
		return out, ErrPrefixSetsDiverge
	}
	return out, nil
}
