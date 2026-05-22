// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package clusterimagepolicymode implements the T8.6 governance gate: the
// admission-policies ClusterImagePolicy.mode may only be 'enforce' when an ADR
// or MEET record signs off the deny-flip; otherwise it must stay 'warn'.
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/check_clusterimagepolicy_mode.sh. Pure-Go (file read +
// regexp) — replaces bash awk/grep. No external CLI. (The "helm"/"cosign" in the
// default paths/comments are a values-file path + a comment, not invocations.)
package clusterimagepolicymode

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Default locations relative to repo root.
const (
	DefaultValuesFile = "infra/helm/admission-policies/values.yaml"
	DefaultADRDir     = ".claude/knowledge-base/decisions"
	DefaultMeetingDir = ".claude/knowledge-base/meetings"
	DefaultLegacyADR  = "docs/meetings/adr"
)

var (
	clusterBlockRe = regexp.MustCompile(`^clusterImagePolicy:`)
	topLevelKeyRe  = regexp.MustCompile(`^[a-zA-Z]`)
	modeLineRe     = regexp.MustCompile(`^[[:space:]]+mode:[[:space:]]*(.+)$`)
	// Sign-off keyword sets (case-sensitive, matching the bash grep -E).
	adrKeywordRe     = regexp.MustCompile(`T8\.6|find_infra_008|clusterimagepolicy.*enforce|deny-flip|deny-mode|T1\.2.*#4|mitigation #4`)
	meetingKeywordRe = regexp.MustCompile(`deny-mode|policyMode.*enforce|T1\.2.*#4|mitigation #4`)
	legacyKeywordRe  = regexp.MustCompile(`T8\.6|find_infra_008`)
)

// Config drives the gate.
type Config struct {
	Root       string
	ValuesFile string // default DefaultValuesFile
	ADRDir     string // default DefaultADRDir
	MeetingDir string // default DefaultMeetingDir
	LegacyADR  string // default DefaultLegacyADR
}

// Outcome captures the result.
type Outcome struct {
	Pass   bool
	Mode   string // "", "warn", "enforce", or other
	Reason string // human-readable explanation
}

// Check evaluates the ClusterImagePolicy.mode governance rule.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	valuesFile := orDefault(cfg.ValuesFile, DefaultValuesFile)
	adrDir := orDefault(cfg.ADRDir, DefaultADRDir)
	meetingDir := orDefault(cfg.MeetingDir, DefaultMeetingDir)
	legacyADR := orDefault(cfg.LegacyADR, DefaultLegacyADR)

	data, err := os.ReadFile(filepath.Join(root, valuesFile))
	if err != nil {
		return &Outcome{Pass: true, Reason: "values.yaml not found (chart not present in this snapshot)"}
	}

	mode := extractMode(string(data))
	switch {
	case mode == "":
		return &Outcome{Pass: true, Reason: "clusterImagePolicy.mode field not present (chart upgrade in progress)"}
	case mode == "warn":
		return &Outcome{Pass: true, Mode: mode, Reason: "mode is 'warn' (Sprint 6 default; Sprint 9 cutover pending)"}
	case mode == "enforce":
		if anyMDMatches(filepath.Join(root, adrDir), adrKeywordRe) {
			return &Outcome{Pass: true, Mode: mode, Reason: "mode 'enforce' with ADR sign-off (knowledge-base/decisions)"}
		}
		if anyMDMatches(filepath.Join(root, meetingDir), meetingKeywordRe) {
			return &Outcome{Pass: true, Mode: mode, Reason: "mode 'enforce' with MEET sign-off (knowledge-base/meetings)"}
		}
		if anyMDMatches(filepath.Join(root, legacyADR), legacyKeywordRe) {
			return &Outcome{Pass: true, Mode: mode, Reason: "mode 'enforce' with legacy ADR sign-off (docs/meetings/adr)"}
		}
		return &Outcome{Pass: false, Mode: mode,
			Reason: "mode 'enforce' but no ADR/MEET record references the deny-flip cutover"}
	default:
		return &Outcome{Pass: false, Mode: mode, Reason: "mode must be 'warn' or 'enforce'"}
	}
}

// extractMode returns the mode value inside the clusterImagePolicy: block, or "".
func extractMode(content string) string {
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		if clusterBlockRe.MatchString(line) {
			inBlock = true
			continue
		}
		if inBlock && topLevelKeyRe.MatchString(line) {
			inBlock = false // left the block on the next top-level key
		}
		if inBlock {
			if m := modeLineRe.FindStringSubmatch(line); m != nil {
				v := strings.TrimSpace(m[1])
				v = strings.Trim(v, `"'`)
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// anyMDMatches reports whether any top-level *.md file in dir matches re.
func anyMDMatches(dir string, re *regexp.Regexp) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if re.Match(data) {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
