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
	"fmt"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/linescan"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// Config tells Check which repo to scan + allows operator override.
type Config struct {
	RepoRoot       string
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

	// Fail-CLOSED (ADR-M24-3): a walk error and a file that cannot be read are
	// both returned, so Check errors instead of computing Pass over a file it
	// never read. A security gate that cannot read what it scans must not
	// report clean.
	var out Outcome
	keep := func(rel string) bool {
		ext := filepath.Ext(rel)
		return (ext == ".yaml" || ext == ".yml") && !allowSet[filepath.Clean(rel)]
	}
	_, err := linescan.Walk(cfg.RepoRoot, scanDirs, keep, func(rel string, data []byte) error {
		hits, err := scanFile(data, rel)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		out.Hits = append(out.Hits, hits...)
		return nil
	})
	if err != nil {
		return out, err
	}
	out.Pass = len(out.Hits) == 0
	return out, nil
}

func scanFile(data []byte, relPath string) ([]Hit, error) {
	lines, err := linescan.Lines(data)
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for i, line := range lines {
		// Strip comments — comment mentioning the type is not a violation.
		if j := strings.Index(line, "#"); j >= 0 {
			line = line[:j]
		}
		if strings.Contains(line, forbiddenType) {
			hits = append(hits, Hit{
				File:       relPath,
				Line:       i + 1,
				Reason:     "legacy SA-token Secret type — static long-lived token forbidden after T7 Vault kubernetes engine migration",
				Suggestion: "remove the Secret; fetch dynamic SA token via Vault kubernetes engine (kubernetes/creds/<role>) with TTL bounded by Vault role policy; see openbao-adoption v1.3 T7",
			})
		}
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
	return sb.String()
}
