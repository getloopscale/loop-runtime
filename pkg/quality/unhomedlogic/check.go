// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package unhomedlogic implements the unbiased migration tracker for plan
// 2026-05-23-operational-logic-capability-architecture: it counts
// "un-homed operational logic" — shell scripts that still carry imperative
// capability that belongs in a professional home (loopzero-ops CLI / declarative
// config / controller / Go-Ginkgo test).
//
// Unbiased by design:
//   - Classification reads the disposition the TEAM already wrote in the
//     allowlist (KEEP-OPERATOR / KEEP-CI-ORCHESTRATOR / MIGRATE-* / AUTO-DIE /
//     REVIEW-NEEDED / DELEGATE-*), plus harness hooks. The gate does not
//     invent classifications — it only counts.
//   - The KPI is a monotonic baseline ratchet: a PR may NEVER increase
//     un-homed logic (total OR any domain). GREEN (target) is reached only by
//     genuinely homing capabilities (delete / delegate), never by relabeling.
//
// This is the RED/GREEN test for the migration: RED today (un-homed > 0),
// ratcheting toward GREEN (un-homed == 0 for migrated domains).
package unhomedlogic

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Disposition is the homed-ness of a script.
type Disposition int

const (
	// Unhomed — imperative capability with no professional home (migration target).
	Unhomed Disposition = iota
	// Homed — justified keep (operator break-glass / CI orchestrator) or harness hook.
	Homed
)

func (d Disposition) String() string {
	if d == Homed {
		return "homed"
	}
	return "unhomed"
}

// Counts is the un-homed tally.
type Counts struct {
	Total    int            `json:"total"`
	ByDomain map[string]int `json:"by_domain"`
	// SuspectKeeps lists scripts tagged KEEP-* but whose body contains
	// reimplementable logic (a Python heredoc) — i.e. a program masquerading
	// as a "kept" operator script. These are counted as un-homed: the KEEP
	// claim is content-invalid. Closes the relabel-to-game-GREEN bias vector.
	SuspectKeeps []string `json:"suspect_keeps,omitempty"`
}

// reimplementableLogicRe detects a Python heredoc — substantial portable logic
// that does NOT belong in a "kept" break-glass/orchestrator script. A genuine
// operator break-glass is shell glue, not an embedded program.
var reimplementableLogicRe = regexp.MustCompile(`(?m)(python3?\s+(-\s+)?<<|<<\s*['"]?(PY|PYEOF|PYTHON))`)

// hasReimplementableLogic reports whether a script body embeds a Python heredoc.
func hasReimplementableLogic(body string) bool {
	return reimplementableLogicRe.MatchString(body)
}

// homedMarkers are the disposition tags that mean "justified keep".
var homedMarkers = []string{"KEEP-OPERATOR", "KEEP-CI-ORCHESTRATOR"}

// classify decides homed-ness from the script path + its allowlist justificativa.
// Harness hooks (.claude/) and runtime container entrypoints are homed; an
// explicit KEEP-* disposition is homed; everything else is un-homed.
func classify(path, justif string) Disposition {
	if strings.HasPrefix(path, ".claude/") {
		return Homed // Claude/CI harness hook
	}
	if strings.HasSuffix(path, "docker-entrypoint.sh") {
		return Homed // container runtime entrypoint
	}
	for _, m := range homedMarkers {
		if strings.Contains(justif, m) {
			return Homed
		}
	}
	return Unhomed
}

// domainOf maps a script path to its capability domain.
func domainOf(path string) string {
	switch {
	case strings.HasPrefix(path, ".claude/"):
		return "harness"
	case strings.Contains(path, "/quality/"):
		return "quality"
	case strings.Contains(path, "/governance/"):
		return "governance"
	case strings.Contains(path, "/chaos/"):
		return "chaos"
	case strings.Contains(path, "/dr/") || strings.Contains(path, "drill"):
		return "drill"
	case strings.Contains(path, "helmfile/scripts/") || strings.Contains(path, "bootstrap"):
		return "bootstrap"
	case strings.HasPrefix(path, "infra/tests/"):
		return "test"
	default:
		return "other"
	}
}

// allowlistRowRe matches a markdown table row whose first column ends in .sh.
var allowlistRowRe = regexp.MustCompile(`^\|\s*([^\s|<>]+\.sh)\s*\|[^|]*\|([^|]*)\|`)

// ReadBody returns a script's content for content-verification. May be nil
// (label-only counting) or return ("", false) when a file is unreadable.
type ReadBody func(path string) (string, bool)

// Count tallies un-homed scripts per domain from allowlist markdown content.
// When readBody is non-nil, a KEEP-tagged script whose body embeds a Python
// heredoc is reclassified as un-homed (a "suspect KEEP") — content beats label.
func Count(allowlistMD string, readBody ReadBody) Counts {
	c := Counts{ByDomain: map[string]int{}}
	for _, line := range strings.Split(allowlistMD, "\n") {
		m := allowlistRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		path := strings.TrimSpace(m[1])
		justif := m[2]
		unhomed := classify(path, justif) == Unhomed

		// Content verification: a KEEP claim is invalid if the body embeds
		// reimplementable logic. Hooks/entrypoints are exempt (genuine glue).
		if !unhomed && readBody != nil && isKeepLabel(justif) {
			if body, ok := readBody(path); ok && hasReimplementableLogic(body) {
				unhomed = true
				c.SuspectKeeps = append(c.SuspectKeeps, path)
			}
		}

		if unhomed {
			c.Total++
			c.ByDomain[domainOf(path)]++
		}
	}
	return c
}

// Entry is one un-homed script's row in the Phase-5 classification ledger.
type Entry struct {
	Path        string
	Domain      string
	LOC         int
	HasPython   bool // embeds a Python heredoc (reimplementable logic)
	ShellsLive  bool // invokes kubectl/k3d/doctl/helm/pulumi/argocd/cosign
	Disposition string
}

var shellsLiveRe = regexp.MustCompile(`\b(kubectl|k3d|doctl|helm|pulumi|argocd|cosign)\b`)

// Ledger emits a proposed disposition + evidence per un-homed script, for
// operator (sponsor) ratification in one pass. Heuristic dispositions — the
// operator corrects; the evidence (python/shells-live/LOC) is mechanical fact.
func Ledger(allowlistMD string, readBody ReadBody) []Entry {
	var entries []Entry
	for _, line := range strings.Split(allowlistMD, "\n") {
		m := allowlistRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		path := strings.TrimSpace(m[1])
		justif := m[2]
		if classify(path, justif) == Homed {
			continue
		}
		e := Entry{Path: path, Domain: domainOf(path)}
		if readBody != nil {
			if body, ok := readBody(path); ok {
				e.LOC = strings.Count(body, "\n")
				e.HasPython = hasReimplementableLogic(body)
				e.ShellsLive = shellsLiveRe.MatchString(body)
			}
		}
		e.Disposition = proposeDisposition(e)
		entries = append(entries, e)
	}
	return entries
}

// proposeDisposition is the heuristic the operator ratifies.
func proposeDisposition(e Entry) string {
	switch {
	case e.HasPython:
		return "MIGRATE-go (python-wrapper — focused TDD)"
	case strings.HasSuffix(e.Path, "_test.sh"):
		return "COUPLED (dies when its target homes)"
	case e.Domain == "drill":
		return "MIGRATE-loopzero-ops-drill (LIVE-gate before delete)"
	case e.Domain == "bootstrap":
		return "DELEGATE-declarative (T2)"
	case e.Domain == "test" || e.Domain == "chaos":
		return "MIGRATE-ginkgo (LIVE)"
	case e.ShellsLive:
		return "KEEP-OPERATOR? (live-cluster tool — operator ratify)"
	default:
		return "MIGRATE-loopzero-ops (pure-logic — autonomous)"
	}
}

// isKeepLabel reports whether the justificativa carries a KEEP-* disposition
// (as opposed to homed-by-path: hooks / container entrypoints).
func isKeepLabel(justif string) bool {
	for _, m := range homedMarkers {
		if strings.Contains(justif, m) {
			return true
		}
	}
	return false
}

// RatchetCheck enforces the monotonic invariant: current must not exceed the
// baseline — neither in total nor in any single domain. Returns the list of
// regressing domains on failure.
func RatchetCheck(current, baseline Counts) (ok bool, regressions []string) {
	if current.Total > baseline.Total {
		regressions = append(regressions,
			fmt.Sprintf("TOTAL un-homed increased %d → %d", baseline.Total, current.Total))
	}
	var domains []string
	for d := range current.ByDomain {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	for _, d := range domains {
		if current.ByDomain[d] > baseline.ByDomain[d] {
			regressions = append(regressions,
				fmt.Sprintf("domain %q un-homed increased %d → %d", d, baseline.ByDomain[d], current.ByDomain[d]))
		}
	}
	return len(regressions) == 0, regressions
}

// Render produces the per-domain report lines.
func (c Counts) Render() []string {
	lines := []string{fmt.Sprintf("un-homed operational logic: %d scripts", c.Total)}
	var domains []string
	for d := range c.ByDomain {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	for _, d := range domains {
		lines = append(lines, fmt.Sprintf("  %-12s %d", d, c.ByDomain[d]))
	}
	return lines
}
