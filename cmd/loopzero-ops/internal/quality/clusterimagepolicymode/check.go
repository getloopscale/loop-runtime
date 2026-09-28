// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package clusterimagepolicymode implements the T8.6 governance gate: the
// admission-policies ClusterImagePolicy.mode may only be 'enforce' when an ADR
// or MEET record signs off the deny-flip; otherwise it must stay 'warn'.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check_clusterimagepolicy_mode.sh. Pure-Go (file read +
// regexp) — replaces bash awk/grep. No external CLI. (The "helm"/"cosign" in the
// default paths/comments are a values-file path + a comment, not invocations.)
package clusterimagepolicymode

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Default locations relative to repo root.
const (
	// DefaultValuesFile is the default of the one admission-policies chart. Every
	// Helmfile cell install overrides the mode from the environment's policyMode and the
	// core install disables the policy, so this default decides only an install that
	// names no mode — which is exactly what an ArgoCD install of the published chart is.
	// It pointed at infra/helm/admission-policies/values.yaml until that copy of the
	// chart was deleted (docs/decisions.md, "One chart per bootstrap/published pair").
	DefaultValuesFile = "charts/loopzero-admission-policies/values.yaml"
	// DefaultDecisionsDoc is where a decision in THIS repository is written: one of the
	// six living documents the 2026-09-11 consolidation left, and versioned, so it reaches
	// whoever clones. It is the only place a sign-off is accepted: the directories the
	// gate used to fall back to were under the gitignored .claude/ tree (or no longer
	// existed), so a sign-off found there held on one machine only.
	DefaultDecisionsDoc = "docs/decisions.md"
)

var (
	clusterBlockRe = regexp.MustCompile(`^clusterImagePolicy:`)
	topLevelKeyRe  = regexp.MustCompile(`^[a-zA-Z]`)
	modeLineRe     = regexp.MustCompile(`^[[:space:]]+mode:[[:space:]]*(.+)$`)
	// signOffRe names the cutover (case-sensitive, matching the bash grep -E).
	signOffRe = regexp.MustCompile(`T8\.6|find_infra_008|clusterimagepolicy.*enforce|deny-flip|deny-mode|T1\.2.*#4|mitigation #4`)
)

// Config drives the gate.
type Config struct {
	Root         string
	DecisionsDoc string // default DefaultDecisionsDoc
	ValuesFile   string // default DefaultValuesFile
}

// Outcome captures the result.
type Outcome struct {
	Pass   bool
	Mode   string // "", "warn", "enforce", or other
	Reason string // human-readable explanation
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check evaluates the ClusterImagePolicy.mode governance rule.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	valuesFile := orDefault(cfg.ValuesFile, DefaultValuesFile)
	decisionsDoc := orDefault(cfg.DecisionsDoc, DefaultDecisionsDoc)

	data, err := os.ReadFile(filepath.Join(root, valuesFile)) // #nosec G304 -- path under the repo root
	if err != nil {
		return &Outcome{Pass: false, Reason: fmt.Sprintf("cannot read %s, so the mode was never checked: %v", valuesFile, err)}
	}

	mode := extractMode(string(data))
	switch mode {
	case "":
		return &Outcome{Pass: true, Reason: "clusterImagePolicy.mode field not present (chart upgrade in progress)"}
	case "warn":
		return &Outcome{Pass: true, Mode: mode, Reason: "mode is 'warn' (Sprint 6 default; Sprint 9 cutover pending)"}
	case "enforce":
		if fileMatches(filepath.Join(root, decisionsDoc), signOffRe) {
			return &Outcome{Pass: true, Mode: mode, Reason: "mode 'enforce' with sign-off in " + decisionsDoc}
		}
		return &Outcome{Pass: false, Mode: mode,
			Reason: "mode 'enforce' but no record references the deny-flip cutover — write it in " + decisionsDoc}
	default:
		return &Outcome{Pass: false, Mode: mode, Reason: "mode must be 'warn' or 'enforce'"}
	}
}

// fileMatches reports whether a single file exists and matches re. Distinct from
// anyMDMatches, which walks a directory: a decision here lives in one document, not in a
// directory of one-decision files.
func fileMatches(path string, re *regexp.Regexp) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the configured doc under the repo root
	if err != nil {
		return false
	}
	return re.Match(data)
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

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
