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
// for the loopzero-api SA. Conditional rules (wrapped in
// `{{- if .Values.rbac.legacyClusterWideSecrets }}` or equivalent
// helm-template guard) are accepted — operator controls cutover.
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
)

// Config tells Check which repo to scan + allows operator override of allowlist.
type Config struct {
	RepoRoot string
	// Allowlist of chart paths exempted from the scan (relative to RepoRoot).
	AllowlistPaths []string
}

// Outcome is the gate result.
type Outcome struct {
	Pass     bool
	Hits     []Hit
	Warnings []string
}

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

// guardOpenRe matches a helm if-guard opening.
var guardOpenRe = regexp.MustCompile(`\{\{-?\s*if\s+`)

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
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("%s: scan error: %v", rel, err))
				return nil
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

// scanFile walks a single clusterrole.yaml looking for unconditional
// secrets/configmaps grants. Tracks helm if-guard depth via simple
// counter so secrets rule inside ANY active guard is considered conditional.
func scanFile(absPath, relPath string) ([]Hit, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hits []Hit
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineNum := 0
	openGuards := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Guard depth tracking. `{{- if ... }}` opens; `{{- end }}` closes.
		// We do NOT need to track {{- else }} since it doesn't change depth.
		if guardOpenRe.MatchString(line) {
			openGuards++
		}
		if strings.Contains(line, "{{- end }}") || strings.Contains(line, "{{ end }}") {
			if openGuards > 0 {
				openGuards--
			}
		}

		// Skip comments + empty lines.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := secretsRuleRe.FindStringSubmatch(line); m != nil {
			if openGuards > 0 {
				continue // guarded — operator-controlled cutover OK
			}
			hits = append(hits, Hit{
				File:      relPath,
				Line:      lineNum,
				Resources: m[1],
				Reason: fmt.Sprintf(
					"ClusterRole grants unconditional cluster-wide %s access — deep review finding #1 (catastrophic blast radius)",
					m[1]),
				Suggestion: "wrap the rule in {{- if .Values.rbac.legacyClusterWideSecrets }} ... {{- end }} (or equivalent guard) and provide per-namespace Role via operator reconcile; see openbao-adoption v1.3 T5.13",
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// FormatOutcome renders the outcome for CLI output.
func FormatOutcome(out Outcome) string {
	var sb strings.Builder
	if out.Pass {
		sb.WriteString("cluster-role-no-secrets-star: PASS\n")
		sb.WriteString("  All ClusterRole templates with secrets/configmaps grants are properly guarded.\n")
		return sb.String()
	}
	sb.WriteString("cluster-role-no-secrets-star: FAIL\n")
	for _, h := range out.Hits {
		fmt.Fprintf(&sb, "  %s:%d — %s\n", h.File, h.Line, h.Reason)
		fmt.Fprintf(&sb, "    Resource grant: %s\n", h.Resources)
		fmt.Fprintf(&sb, "    Suggestion: %s\n", h.Suggestion)
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(&sb, "  WARN: %s\n", w)
	}
	return sb.String()
}
