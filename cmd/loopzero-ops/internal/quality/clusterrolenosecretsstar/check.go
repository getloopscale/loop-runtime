// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package clusterrolenosecretsstar is the Go home for the
// cluster-role-no-secrets-star quality gate (T5.15 of plan
// 2026-06-02-openbao-adoption v1.3).
//
// Closes deep review finding #1: loopzero-api SA cluster-wide
// secrets/configmaps grant = catastrophic blast radius.
//
// The gate walks every `templates/clusterrole.yaml` under `charts/` and
// `infra/helm/` and asserts: no ClusterRole template emits an
// UNCONDITIONAL `secrets` or `configmaps` rule with `apiGroups: [""]`
// for the loopzero-api SA. A rule is accepted as conditional ONLY when it sits
// inside `{{- if .Values.rbac.legacyClusterWideSecrets }}` — the declared
// cutover guard (cutoverGuardRe). Any other enclosing block, notably the
// chart-wide `{{- if .Values.rbac.create }}` wrapper every operator chart
// uses, decides whether the ClusterRole exists at all and says nothing about
// the grant, so it does not count.
//
// Defense in depth: even if a values.yaml later flips legacyClusterWideSecrets
// to true in production, the gate catches new charts that introduce the
// pattern unguarded.
package clusterrolenosecretsstar

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// Config tells Check which repo to scan + allows operator override of allowlist.
type Config struct {
	RepoRoot string
	// Allowlist of chart paths exempted from the scan (relative to RepoRoot).
	AllowlistPaths []string
}

// Outcome is the gate result.
type Outcome struct {
	Pass bool
	Hits []Hit
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Hit is a single violation location.
type Hit struct {
	File       string
	Line       int
	Resources  string
	Reason     string
	Suggestion string
}

var scanDirs = []string{"charts", "infra/helm"}

// secretsRuleRe matches `resources: ["secrets", "configmaps"]` and variants.
var secretsRuleRe = regexp.MustCompile(`resources:\s*\[[^\]]*(secrets|configmaps)[^\]]*\]`)

// blockOpenRe matches every template action that opens a block closed by
// `end`. All of them must be tracked, or an `end` closing a range would be
// read as closing the guard around it.
var blockOpenRe = regexp.MustCompile(`\{\{-?\s*(if|with|range|define|block)\s`)

// blockElseRe matches an `else` / `else if` of the innermost block.
var blockElseRe = regexp.MustCompile(`\{\{-?\s*else\b`)

// blockEndRe matches the action that closes the innermost block.
var blockEndRe = regexp.MustCompile(`\{\{-?\s*end\s*-?\}\}`)

// cutoverGuardRe is the ONLY guard that makes a cluster-wide secrets or
// configmaps grant operator-controlled (openbao-adoption v1.3 T5.13).
var cutoverGuardRe = regexp.MustCompile(`\{\{-?\s*if\s+\.Values\.rbac\.legacyClusterWideSecrets\s*-?\}\}`)

// Check scans the configured RepoRoot and returns an Outcome.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		return Outcome{}, fmt.Errorf("clusterrolenosecretsstar: empty RepoRoot")
	}
	allowSet := make(map[string]bool)
	for _, p := range cfg.AllowlistPaths {
		allowSet[filepath.Clean(p)] = true
	}

	var out Outcome
	for _, dir := range scanDirs {
		abs := filepath.Join(cfg.RepoRoot, dir)
		if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
			continue // scan dir legitimately absent — nothing to scan
		}
		// FIX-2 (M24, ADR-M24-3): fail-CLOSED — capture + propagate the walk error.
		walkErr := filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if filepath.Base(path) != "clusterrole.yaml" {
				return nil
			}
			rel, _ := filepath.Rel(cfg.RepoRoot, path)
			if allowSet[filepath.Clean(rel)] {
				return nil
			}
			hits, err := scanFile(path, rel)
			if err != nil {
				// Fail closed: a template the gate could not read is a
				// template nobody audited, never a clean one.
				return fmt.Errorf("scan %s: %w", rel, err)
			}
			out.Hits = append(out.Hits, hits...)
			return nil
		})
		if walkErr != nil {
			return out, fmt.Errorf("scan %s: %w", dir, walkErr)
		}
	}
	out.Pass = len(out.Hits) == 0
	return out, nil
}

// scanFile walks a single clusterrole.yaml looking for cluster-wide
// secrets/configmaps grants that are not inside the cutover guard. It keeps a
// stack of open template blocks; each frame records whether it is the
// cutover guard's true branch.
func scanFile(absPath, relPath string) ([]Hit, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hits []Hit
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var frames []bool // true = inside the cutover guard's true branch
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		frames = trackBlocks(frames, line)

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := secretsRuleRe.FindStringSubmatch(line)
		if m == nil || insideCutover(frames) {
			continue
		}
		hits = append(hits, Hit{
			File:      relPath,
			Line:      lineNum,
			Resources: m[1],
			Reason: fmt.Sprintf(
				"ClusterRole grants cluster-wide %s access outside the rbac.legacyClusterWideSecrets cutover guard — deep review finding #1 (catastrophic blast radius)",
				m[1]),
			Suggestion: "wrap the rule in {{- if .Values.rbac.legacyClusterWideSecrets }} ... {{- end }} and provide per-namespace Role via operator reconcile; see openbao-adoption v1.3 T5.13",
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// trackBlocks applies the block actions on one line to the frame stack.
func trackBlocks(frames []bool, line string) []bool {
	if blockOpenRe.MatchString(line) {
		frames = append(frames, cutoverGuardRe.MatchString(line))
	}
	if blockElseRe.MatchString(line) && len(frames) > 0 {
		frames[len(frames)-1] = false
	}
	if blockEndRe.MatchString(line) && len(frames) > 0 {
		frames = frames[:len(frames)-1]
	}
	return frames
}

// insideCutover reports whether any open frame is the cutover guard.
func insideCutover(frames []bool) bool {
	for _, f := range frames {
		if f {
			return true
		}
	}
	return false
}

// FormatOutcome renders the outcome for CLI output.
func FormatOutcome(out Outcome) string {
	var sb strings.Builder
	if out.Pass {
		sb.WriteString("cluster-role-no-secrets-star: PASS\n")
		sb.WriteString("  Every cluster-wide secrets/configmaps grant sits inside the rbac.legacyClusterWideSecrets cutover guard.\n")
		return sb.String()
	}
	sb.WriteString("cluster-role-no-secrets-star: FAIL\n")
	for _, h := range out.Hits {
		fmt.Fprintf(&sb, "  %s:%d — %s\n", h.File, h.Line, h.Reason)
		fmt.Fprintf(&sb, "    Resource grant: %s\n", h.Resources)
		fmt.Fprintf(&sb, "    Suggestion: %s\n", h.Suggestion)
	}
	return sb.String()
}
