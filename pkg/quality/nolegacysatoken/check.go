// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package nolegacysatoken is the Go home for the no-legacy-sa-token-secret
// quality gate (T7.6 of plan 2026-06-02-openbao-adoption v1.3).
//
// Forbids `type: kubernetes.io/service-account-token` Secret resources
// anywhere in chart templates. These are LONG-LIVED static tokens —
// after T7 migration, all SA tokens MUST come from Vault kubernetes engine
// (short-lived TTL bounded JWTs signed by K8s issuer).
package nolegacysatoken

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config tells Check which repo to scan + allows operator override.
type Config struct {
	RepoRoot       string
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
	Reason     string
	Suggestion string
}

var scanDirs = []string{"charts", "infra/helm", "infra/manifests"}

const forbiddenType = "kubernetes.io/service-account-token"

// Check scans the configured RepoRoot and returns an Outcome.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		return Outcome{}, fmt.Errorf("nolegacysatoken: empty RepoRoot")
	}
	allowSet := make(map[string]bool)
	for _, p := range cfg.AllowlistPaths {
		allowSet[filepath.Clean(p)] = true
	}

	var out Outcome
	for _, dir := range scanDirs {
		abs := filepath.Join(cfg.RepoRoot, dir)
		_ = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".yaml" && ext != ".yml" {
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
	}
	out.Pass = len(out.Hits) == 0
	return out, nil
}

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
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		// Strip comments — comment mentioning the type is not a violation.
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if strings.Contains(line, forbiddenType) {
			hits = append(hits, Hit{
				File: relPath,
				Line: lineNum,
				Reason: "legacy SA-token Secret type — static long-lived token forbidden after T7 Vault kubernetes engine migration",
				Suggestion: "remove the Secret; fetch dynamic SA token via Vault kubernetes engine (kubernetes/creds/<role>) with TTL bounded by Vault role policy; see openbao-adoption v1.3 T7",
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
		sb.WriteString("no-legacy-sa-token-secret: PASS\n")
		sb.WriteString("  No 'type: kubernetes.io/service-account-token' literals found in chart templates.\n")
		return sb.String()
	}
	sb.WriteString("no-legacy-sa-token-secret: FAIL\n")
	for _, h := range out.Hits {
		fmt.Fprintf(&sb, "  %s:%d — %s\n", h.File, h.Line, h.Reason)
		fmt.Fprintf(&sb, "    Suggestion: %s\n", h.Suggestion)
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(&sb, "  WARN: %s\n", w)
	}
	return sb.String()
}
