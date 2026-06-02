// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package openbaoprodsafe is the Go home for the openbao-prod-safe gate
// (F1.9 of plan 2026-06-02-openbao-adoption v1.2). Enforces:
//
//  1. infra/helm/openbao/values-prod.yaml has NO dev literals:
//     - loopzero-dev-* (any string starting with this)
//     - devMode.enabled: true
//     - rootToken non-empty
//
//  2. infra/helm/openbao/values-prod.yaml + values.yaml image.tag NOT in
//     forbidden floating-tag list: latest, main, edge, devel, stable
//     (Renovate must pin semver tag; SHA digest preferred).
//
//  3. infra/helm/openbao/templates/vap-dev-mode.yaml exists (EC-04
//     ValidatingAdmissionPolicy with matchConditions excluding system
//     controllers per B33 pattern).
//
// Per MEET-ASYNC-AMENDMENT-2026-06-02-001-openbao-f1-execution.md Q9-F1:
// "VAP rejeita devMode em prod NS" — this gate is the static
// (build-time) complement to the runtime VAP enforcement.
//
// Per Q6-F1 coverage threshold: this package targets 95%+ coverage.
package openbaoprodsafe

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Skipped    bool // chart absent
	Violations []string
}

// Forbidden dev literals in values-prod.yaml
var (
	devLiteralPatterns = []*regexp.Regexp{
		regexp.MustCompile(`loopzero-dev-[a-zA-Z0-9-]+`),
	}
	forbiddenTags = []string{
		"latest",
		"main",
		"edge",
		"devel",
		"stable",
	}
	// devMode.enabled: true — YAML pattern (allowing varied whitespace)
	devModeEnabledRe = regexp.MustCompile(`(?m)^\s*enabled:\s*true\s*$`)
	// rootToken with non-empty value (rejects "" but allows commented line)
	rootTokenNonEmptyRe = regexp.MustCompile(`(?m)^\s*rootToken:\s*"?[^"\s]`)
)

// Check fails if openbao/values-prod.yaml contains dev literals OR
// forbidden floating image tags OR if vap-dev-mode.yaml is missing.
func Check(cfg Config) (*Outcome, error) {
	chartDir := filepath.Join(cfg.RepoRoot, "infra", "helm", "openbao")
	if fi, err := os.Stat(chartDir); err != nil || !fi.IsDir() {
		return &Outcome{Pass: true, Skipped: true}, nil
	}

	out := &Outcome{Pass: true}

	// 1. Scan values-prod.yaml for dev literals + forbidden tags
	prodValuesPath := filepath.Join(chartDir, "values-prod.yaml")
	if _, err := os.Stat(prodValuesPath); err == nil {
		body, err := os.ReadFile(prodValuesPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", prodValuesPath, err)
		}
		text := string(body)

		// Dev literals (loopzero-dev-*)
		for _, re := range devLiteralPatterns {
			if matches := re.FindAllString(text, -1); len(matches) > 0 {
				for _, m := range matches {
					out.Pass = false
					out.Violations = append(out.Violations,
						fmt.Sprintf("%s: forbidden dev literal '%s' (only allowed in values-standalone.yaml)",
							relPath(cfg.RepoRoot, prodValuesPath), m))
				}
			}
		}

		// devMode.enabled: true — only inspect blocks under devMode:
		if hasDevModeEnabledTrue(text) {
			out.Pass = false
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s: 'devMode.enabled: true' is forbidden in prod values",
					relPath(cfg.RepoRoot, prodValuesPath)))
		}

		// rootToken non-empty in prod
		if hasRootTokenNonEmpty(text) {
			out.Pass = false
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s: 'rootToken' must be empty in prod values (use '' or comment)",
					relPath(cfg.RepoRoot, prodValuesPath)))
		}

		// Forbidden image tags
		if tag := extractImageTag(text); tag != "" {
			for _, forbidden := range forbiddenTags {
				if tag == forbidden {
					out.Pass = false
					out.Violations = append(out.Violations,
						fmt.Sprintf("%s: image.tag='%s' is forbidden (Renovate pins semver + SHA)",
							relPath(cfg.RepoRoot, prodValuesPath), tag))
				}
			}
		}
	}

	// 2. Scan values.yaml (shared defaults) for forbidden tags
	sharedValuesPath := filepath.Join(chartDir, "values.yaml")
	if _, err := os.Stat(sharedValuesPath); err == nil {
		body, err := os.ReadFile(sharedValuesPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", sharedValuesPath, err)
		}
		if tag := extractImageTag(string(body)); tag != "" {
			for _, forbidden := range forbiddenTags {
				if tag == forbidden {
					out.Pass = false
					out.Violations = append(out.Violations,
						fmt.Sprintf("%s: image.tag='%s' is forbidden (Renovate pins semver + SHA)",
							relPath(cfg.RepoRoot, sharedValuesPath), tag))
				}
			}
		}
	}

	// 3. VAP template existence (EC-04)
	vapPath := filepath.Join(chartDir, "templates", "vap-dev-mode.yaml")
	if _, err := os.Stat(vapPath); err != nil {
		out.Pass = false
		out.Violations = append(out.Violations,
			fmt.Sprintf("%s: missing — EC-04 requires ValidatingAdmissionPolicy blocking devMode in prod NS",
				relPath(cfg.RepoRoot, vapPath)))
	} else {
		// VAP must include matchConditions excluding system controllers (B33)
		vapBody, _ := os.ReadFile(vapPath)
		vapText := string(vapBody)
		if !strings.Contains(vapText, "matchConditions") {
			out.Pass = false
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s: missing matchConditions (B33: VAP without matchConditions excluding namespace-controller deadlocks ns delete)",
					relPath(cfg.RepoRoot, vapPath)))
		}
		if !strings.Contains(vapText, "namespace-controller") {
			out.Pass = false
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s: matchConditions must explicitly exclude system:serviceaccount:kube-system:namespace-controller (B33 pattern)",
					relPath(cfg.RepoRoot, vapPath)))
		}
	}

	return out, nil
}

// hasDevModeEnabledTrue searches for `devMode:` block followed by
// `enabled: true` within the next ~5 lines (YAML indent-aware).
func hasDevModeEnabledTrue(text string) bool {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "devMode:" || strings.HasPrefix(trimmed, "devMode:") {
			// Inspect next 5 lines for `enabled: true`
			for j := i + 1; j < len(lines) && j < i+6; j++ {
				next := strings.TrimSpace(lines[j])
				// Stop at next sibling YAML key (no indentation increase)
				if next != "" && !strings.HasPrefix(lines[j], " ") && !strings.HasPrefix(lines[j], "\t") {
					break
				}
				if devModeEnabledRe.MatchString(lines[j]) {
					return true
				}
			}
		}
	}
	return false
}

// hasRootTokenNonEmpty searches for rootToken: <non-empty-non-quoted-empty>.
func hasRootTokenNonEmpty(text string) bool {
	return rootTokenNonEmptyRe.MatchString(text)
}

// extractImageTag extracts the image.tag value from values YAML.
// Returns empty string if not found.
func extractImageTag(text string) string {
	lines := strings.Split(text, "\n")
	inImage := false
	imageIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if trimmed == "image:" {
			inImage = true
			imageIndent = indent
			continue
		}
		if inImage {
			// Exit image block on lower-or-equal indent non-empty line
			if trimmed != "" && indent <= imageIndent && !strings.HasPrefix(trimmed, "#") {
				inImage = false
				continue
			}
			if strings.HasPrefix(trimmed, "tag:") {
				val := strings.TrimSpace(strings.TrimPrefix(trimmed, "tag:"))
				val = strings.Trim(val, "\"'")
				return val
			}
		}
	}
	return ""
}

func relPath(root, abs string) string {
	if rel, err := filepath.Rel(root, abs); err == nil {
		return rel
	}
	return abs
}
