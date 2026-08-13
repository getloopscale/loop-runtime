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
// Equal sets are not an equal rule, and TWO watched copies are not the whole
// rule. A reader who sees this gate green must not conclude "the rule is
// unified". Corrected 2026-08-12 after an independent review measured what the
// original text got wrong:
//
//   - The SEVERITY axis is out of scope (B-069): one copy discards silently, the
//     other kills the build.
//   - The SET axis is watched but admits DATED exemptions (allowlist.txt).
//   - The CASE axis is watched since B-070, with the evasions listed honestly in
//     casematch.go rather than implied not to exist.
//   - api/internal/source declares a similar-looking list that is NOT a copy and
//     is deliberately out of scope. Measured 2026-08-13: it blocks PROCESS
//     HIJACK — LD_PRELOAD and DYLD_* load an attacker's library before the real
//     one, PATH/SHELL redirect what runs — while find_sec_004 blocks SHADOWING
//     of platform-injected config. Two threats, overlapping on LOOPZERO_ and
//     KUBERNETES_ for different reasons. Comparing them here would need four
//     exemptions, each asserting a divergence that is not one. It carried the
//     same symbol name until 2026-08-13, and that collision is what made it read
//     as a third copy (B-089, ADR 2026-08-13-reserved-env-two-rules).
//
// Saying all of this here is cheaper than a green gate that quietly overstates
// itself, which is the exact defect the slice this gate belongs to exists to
// remove — and which this very doc-comment committed in its first version.
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
	"time"
)

// dedupe collapses repeats in a sorted slice.
func dedupe(in []string) []string {
	var out []string
	for i, v := range in {
		if i == 0 || in[i-1] != v {
			out = append(out, v)
		}
	}
	return out
}

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

// Config is the gate input. The paths are explicit rather than discovered: a
// discovery mechanism is the speculative generality rung 1 of the parsimony
// ladder refuses, and adding a site is a decision, not a lookup.
//
// The original text here claimed "exactly two copies today (measured)". Two
// corrections, in this order. First it was called wrong, because a third
// declaration of the same NAME exists in api/internal/source. Then that was
// corrected in turn: the two ARE the only copies — the third is a different rule
// (process hijack, not shadowing), and it shared the name until 2026-08-13. See
// ADR docs/wiki/decisions/2026-08-13-reserved-env-two-rules.md and the file
// header above. Listing it here would need four exemptions, each asserting a
// divergence that is not one.
type Config struct {
	RepoRoot string
	Sites    []Site
	// AllowlistPath overrides DefaultAllowlistRelPath (tests).
	AllowlistPath string
	// Now overrides the clock for sunset evaluation (tests). Zero means real time.
	Now time.Time
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
	// Exempted lists set-axis divergences covered by an unexpired allowlist
	// entry. Reported rather than hidden: an exemption nobody sees is a
	// divergence nobody retires.
	Exempted []string
	// CaseFindings lists case-folding calls inside a prefix-matching loop.
	CaseFindings []CaseFinding
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

	now := cfg.Now
	if now.IsZero() {
		now = time.Now()
	}
	exempt, _, aerr := loadExemptions(cfg.RepoRoot, cfg.AllowlistPath, now)
	if aerr != nil {
		return Outcome{}, aerr
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
			if !missingElsewhere {
				continue
			}
			if exempt[p] {
				out.Exempted = append(out.Exempted, p)
				continue
			}
			only = append(only, p)
		}
		if len(only) > 0 {
			sort.Strings(only)
			out.OnlyIn[path] = only
			diverged = true
		}
	}
	// The CASE axis has no exemption: it was decided, and a decided axis that
	// nothing watches is one edit away from being undecided again.
	caseFindings, cerr := checkCaseMatching(cfg.RepoRoot, cfg.Sites)
	if cerr != nil {
		return out, cerr
	}
	out.CaseFindings = caseFindings

	sort.Strings(out.Exempted)
	out.Exempted = dedupe(out.Exempted)

	if len(out.CaseFindings) > 0 {
		return out, ErrCaseFoldingInMatch
	}
	if diverged {
		return out, ErrPrefixSetsDiverge
	}
	return out, nil
}
