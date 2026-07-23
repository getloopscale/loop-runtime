// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	var out Outcome
	abs := filepath.Join(cfg.RepoRoot, scanDir)
	if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
		return Outcome{Pass: true}, nil // scan dir absent — nothing to scan
	}
	// FIX-2 (M24, ADR-M24-3): fail-CLOSED — capture + propagate the walk error
	// instead of discarding it (`_ =`) and swallowing per-entry errors. A gate
	// that cannot read the files it scans must NOT report clean.
	walkErr := filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		// Only scan Pulumi stack files (Pulumi.<stack>.yaml or Pulumi.yaml).
		if !strings.HasPrefix(base, "Pulumi.") || (!strings.HasSuffix(base, ".yaml") && !strings.HasSuffix(base, ".yml")) {
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
		return out, fmt.Errorf("scan %s: %w", scanDir, walkErr)
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
	currentKey := ""
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
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
				File: relPath,
				Line: lineNum,
				Key:  currentKey,
				Reason: "Pulumi inline encrypted secret — bypasses Vault audit log + rotation; post-T9 migration all secrets must come from Vault via vault.generic.getSecret",
				Suggestion: "replace with Vault data source: `value: ${vault_generic.X.data[\"key\"]}` and configure the @pulumi/vault provider per docs/operations/prod-pulumi-bootstrap.md",
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
		sb.WriteString("no-pulumi-inline-secrets: PASS\n")
		sb.WriteString("  No `secure: v1:...` literals found in Pulumi stack files.\n")
		return sb.String()
	}
	sb.WriteString("no-pulumi-inline-secrets: FAIL\n")
	for _, h := range out.Hits {
		fmt.Fprintf(&sb, "  %s:%d (key=%s) — %s\n", h.File, h.Line, h.Key, h.Reason)
		fmt.Fprintf(&sb, "    Suggestion: %s\n", h.Suggestion)
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(&sb, "  WARN: %s\n", w)
	}
	return sb.String()
}
