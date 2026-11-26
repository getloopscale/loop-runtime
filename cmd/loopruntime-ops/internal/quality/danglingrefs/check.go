// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package danglingrefs implements the T0.1 backstop gate from
// 2026-05-23-incremental-bash-to-go-migration-plan.
//
// It fails when a .sh script DELETED (or renamed-away) in the diff is still
// referenced — in a live, non-comment context — by any caller surface:
// Taskfile.yml, taskfiles/, .github/workflows/, infra/. The installed kit
// under .claude/ is gitignored and is not a surface: a gate reading it would
// answer differently on every clone.
//
// This is the automated backstop against the failure that abandoned the
// develop-bkp big-bang: scripts were deleted faster than their callers were
// rewired, and unit-test-green masked the broken drill/cleanroom/chaos SKILLs.
//
// Design notes:
//   - Basename-collision guard: a deleted `dr/preflight.sh` must NOT match a
//     surviving `tests/parity/preflight.sh`. We match the full path AND the
//     `<parentdir>/<basename>` suffix form the SKILLs use — both carry the
//     parent dir, so unrelated same-basename files never collide.
//   - Comment/provenance exclusion: lines that are pure comments or carry
//     "Migrated from"/"replaced by"/"per plan" provenance are not "live".
//   - EC-04 parity with nonewbash: base-ref unreachable fails LOUD.
package danglingrefs

import (
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/deletedrefs"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// Config tells Check where to operate.
type Config struct {
	RepoRoot string // absolute path to repo root
	BaseRef  string // base git ref (e.g., "origin/develop")
	HeadRef  string // head git ref (e.g., "HEAD")
}

// Outcome captures the result of a Check invocation.
type Outcome struct {
	Pass       bool
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// excludedPathFragments are files that legitimately NAME a deleted script
// without being a live caller (the allowlist, the audit CSV).
var excludedPathFragments = []string{
	"shell-scripts-allowlist.md",
	"scripts-audit",
}

// liveCallContext matches lines that INVOKE/REFERENCE a script in an
// executable or operator-instruction context (a backticked path or a list
// item counts too).
var liveCallContext = regexp.MustCompile(`(?i)(\bbash\b|\bsource\b|^\s*\.\s|run:|\bexec\b|script=|/tmp/loopruntime-ops|` + "`" + `|^\s*-\s|\.sh)`)

// spec is this gate's configuration of the shared dangling-reference engine.
var spec = deletedrefs.Spec{
	Noun:         "script",
	DiffPathspec: []string{"*.sh"},
	Surfaces: []string{
		"Taskfile.yml",
		"taskfiles",
		".github/workflows",
		"infra",
	},
	IsCaller: isCaller,
	// Comment lines that document a migration rather than invoke the script.
	ProvenanceMarkers: []string{
		"migrated from", "migrated to", "replaced by", "per plan",
		"superseded", "see d", "lesson",
	},
	LiveContext: liveCallContext,
}

// Check is the entry point for `loopruntime-ops quality dangling-script-refs`.
// Error is reserved for INFRASTRUCTURE failures (git missing, base-ref
// unreachable); dangling references come back via Outcome.Violations.
func Check(cfg Config) (*Outcome, error) {
	violations, err := deletedrefs.Violations(spec, cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef)
	if err != nil {
		return nil, err
	}
	return &Outcome{Pass: len(violations) == 0, Violations: violations}, nil
}

// isCaller reports whether a repo-relative file under a surface can be a live
// caller of a script.
func isCaller(rel string) bool {
	lower := strings.ToLower(rel)
	for _, frag := range excludedPathFragments {
		if strings.Contains(lower, frag) {
			return false
		}
	}
	// CSV/TXT are data/baseline/documentation files, not live callers.
	if strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".txt") {
		return false
	}
	// Markdown prose is not a live caller.
	return !strings.HasSuffix(lower, ".md")
}
