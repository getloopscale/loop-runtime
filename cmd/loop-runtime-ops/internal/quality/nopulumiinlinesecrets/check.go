// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package nopulumiinlinesecrets is the Go home for the no-pulumi-inline-secrets
// quality gate (T9.6 of plan 2026-06-02-openbao-adoption v1.3).
//
// Forbids `secure: v1:...` encrypted entries in Pulumi.*.yaml stack files.
// After T9 migration, all secrets MUST be sourced from Vault via
// @pulumi/vault provider (vault.generic.getSecret). Inline encrypted values
// require sharing the passphrase out-of-band, fail to support rotation, and
// bypass Vault audit log streaming.
package nopulumiinlinesecrets

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/linescan"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
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
	Key        string
	Reason     string
	Suggestion string
}

// scanDir is the Pulumi project tree.
var scanDir = "infra/pulumi"

// Marker for Pulumi encrypted secrets.
const inlineSecretMarker = "secure: v1:"

// Check scans the Pulumi tree for inline encrypted secrets.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		return Outcome{}, fmt.Errorf("nopulumiinlinesecrets: empty RepoRoot")
	}
	allowSet := make(map[string]bool)
	for _, p := range cfg.AllowlistPaths {
		allowSet[filepath.Clean(p)] = true
	}

	// Fail-CLOSED (ADR-M24-3): a walk error and a stack file that cannot be read
	// are both returned, so Check errors instead of computing Pass over a file it
	// never read. A gate that cannot read the files it scans must not report clean.
	var out Outcome
	keep := func(rel string) bool {
		base := filepath.Base(rel)
		// Only scan Pulumi stack files (Pulumi.<stack>.yaml or Pulumi.yaml).
		if !strings.HasPrefix(base, "Pulumi.") || (!strings.HasSuffix(base, ".yaml") && !strings.HasSuffix(base, ".yml")) {
			return false
		}
		return !allowSet[filepath.Clean(rel)]
	}
	walked, err := linescan.Walk(cfg.RepoRoot, []string{scanDir}, keep, func(rel string, data []byte) error {
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
	if walked.Dirs == 0 {
		return Outcome{Pass: true}, nil // scan dir absent — nothing to scan
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
	currentKey := ""
	for i, line := range lines {
		lineNum := i + 1
		// Track the most recent top-level key.
		trimmed := strings.TrimSpace(line)
		// A key line under config: looks like "key: value" with consistent indent.
		// We only need the marker on the same OR following line — track key for context.
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.HasSuffix(trimmed, ":") {
			// Top-level marker (config: or similar) — reset.
			currentKey = strings.TrimSuffix(trimmed, ":")
		} else if strings.Contains(line, ":") && !strings.Contains(line, inlineSecretMarker) {
			// Nested key — capture for next-line marker detection.
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				currentKey = parts[0]
			}
		}
		if strings.Contains(line, inlineSecretMarker) {
			hits = append(hits, Hit{
				File:       relPath,
				Line:       lineNum,
				Key:        currentKey,
				Reason:     "Pulumi inline encrypted secret — bypasses Vault audit log + rotation; post-T9 migration all secrets must come from Vault via vault.generic.getSecret",
				Suggestion: "replace with Vault data source: `value: ${vault_generic.X.data[\"key\"]}` and configure the @pulumi/vault provider per docs/runbooks.md",
			})
		}
	}
	return hits, nil
}

// FormatOutcome renders the outcome for CLI output.
func FormatOutcome(out Outcome) string {
	var sb strings.Builder
	if out.Pass {
		sb.WriteString("no-pulumi-inline-secrets: PASS\n")
		sb.WriteString("  No `secure: v1:...` literals found in Pulumi stack files.\n")
		return sb.String()
	}
	sb.WriteString("no-pulumi-inline-secrets: FAIL\n")
	for _, h := range out.Hits {
		fmt.Fprintf(&sb, "  %s:%d (key=%s) — %s\n", h.File, h.Line, h.Key, h.Reason)
		fmt.Fprintf(&sb, "    Suggestion: %s\n", h.Suggestion)
	}
	return sb.String()
}
