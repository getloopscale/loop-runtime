// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package danglingdata is the data-file twin of cmd/loopzero-ops/internal/quality/danglingrefs,
// both configurations of the shared deletedrefs engine. Where
// danglingrefs catches a DELETED SCRIPT still referenced by a live caller, this
// gate catches a DELETED DATA FILE (config/catalog .yaml/.yml/.json) still
// referenced by a live executable caller (.sh script or workflow/Taskfile YAML).
//
// Both close the same integration-boundary failure class:
//   - develop-bkp (2026-05-23): deleted scripts, callers not rewired.
//   - governance subsystem (2026-05-23): the 2026-05-11 consolidation deleted
//     the maturity-scoring DATA (docs/governance/, docs/evidence/, ...) but left
//     the scripts that read it dangling — verify-independence always exit 2,
//     health-check-governance always exit 1. See
//     finding-2026-05-23-governance-subsystem-orphaned.md.
//
// Scope is deliberately TIGHT to keep false positives near zero:
//   - Deleted side: only .yaml/.yml/.json, and NOT under code/chart/fixture
//     trees (infra/helm, charts, */fixtures, */testdata, */migrations, api,
//     operators, cli, pkg, cmd) where data deletion is routine + self-contained
//     (consumed via globs/frameworks, not hardcoded script paths).
//   - Caller side: only EXECUTABLE orchestration — .sh anywhere, or .yml/.yaml
//     under .github/workflows / taskfiles / Taskfile.yml. Prose (.md) and
//     ordinary manifests/values are NOT callers.
//   - Reference must be live (non-comment, non-provenance).
package danglingdata

import (
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deletedrefs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Config tells Check where to operate.
type Config struct {
	RepoRoot string
	BaseRef  string
	HeadRef  string
}

// Outcome captures the result of a Check invocation.
type Outcome struct {
	Pass       bool
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// dataExtensions are the deleted-file types worth checking (config/data inputs
// a script might hardcode). .md is excluded — prose deletion is routine and
// danglingrefs already treats .md as non-caller.
var dataExtensions = []string{".yaml", ".yml", ".json"}

// deletedExcludedPrefixes: deletions under these trees are routine + self-
// contained (Helm charts, code, generated). A script wouldn't hardcode them as
// a config input; they're consumed by frameworks via globs.
var deletedExcludedPrefixes = []string{
	"infra/helm/", "charts/", "api/", "operators/", "cli/", "pkg/", "cmd/",
	"vendor/", "node_modules/",
}

// deletedExcludedFragments: path fragments marking fixture/test/generated data.
var deletedExcludedFragments = []string{
	"/fixtures/", "/testdata/", "/snapshots/", "/__tests__/", "/test_helpers/",
	"/migrations/",
}

// spec is this gate's configuration of the shared dangling-reference engine.
var spec = deletedrefs.Spec{
	Noun:     "data file",
	InScope:  isDataInScope,
	Surfaces: []string{"Taskfile.yml", "taskfiles", ".github/workflows", "infra", "scripts", "tools"},
	IsCaller: isCallerFile,
	// Documentation/comment lines, not live references.
	ProvenanceMarkers: []string{
		"migrated from", "migrated to", "replaced by", "per plan",
		"superseded", "was:", "deleted", "removed",
	},
}

// Check is the entry point for `loopzero-ops quality dangling-data-refs`.
func Check(cfg Config) (*Outcome, error) {
	violations, err := deletedrefs.Violations(spec, cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef)
	if err != nil {
		return nil, err
	}
	return &Outcome{Pass: len(violations) == 0, Violations: violations}, nil
}

// isDataInScope reports whether a deleted path is a config/data file outside the
// routine-churn trees.
func isDataInScope(p string) bool {
	hasDataExt := false
	for _, ext := range dataExtensions {
		if strings.HasSuffix(p, ext) {
			hasDataExt = true
			break
		}
	}
	if !hasDataExt {
		return false
	}
	for _, pre := range deletedExcludedPrefixes {
		if strings.HasPrefix(p, pre) {
			return false
		}
	}
	for _, frag := range deletedExcludedFragments {
		if strings.Contains(p, frag) {
			return false
		}
	}
	return true
}

// isCallerFile reports whether a repo-relative file is executable
// orchestration: any .sh, or a .yml/.yaml under .github/workflows / taskfiles,
// or a Taskfile.yml. Ordinary manifests/values and .md prose are NOT callers.
func isCallerFile(rel string) bool {
	if strings.HasSuffix(rel, ".sh") {
		return true
	}
	if strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml") {
		return strings.Contains(rel, ".github/workflows/") ||
			strings.HasPrefix(rel, "taskfiles/") ||
			strings.Contains(rel, "/taskfiles/") ||
			strings.HasSuffix(rel, "Taskfile.yml")
	}
	return false
}
