// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package baseline implements the coverage baseline JSON read/write
// primitives consumed by Phase 5 baseline gates (T5.12 baseline-compare,
// T5.16 baseline-update). Replaces the JSON parsing shared between
// infra/scripts/quality/baseline-{compare,update}.sh per plan
// 2026-05-21-bash-to-go-migration-plan Phase 5.
//
// Origin: plan 2026-05-21-comprehensive-quality-gates v1.1 T1.2 + T2.2.
//
// Schema: { "packages": { "<pkg>": { "coverage": 87.5, "min_required": 80 } } }
//
// Critical packages (D11) get min_required pinned to 90 and never
// auto-downgraded. The pinned list is shared between compare + update
// to ensure consistent enforcement.
package baseline

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// CriticalPackages — D11 list — Min_required pinned to 90, never
// auto-downgraded by baseline-update. Order is alphabetical for
// deterministic test fixtures.
var CriticalPackages = []string{
	"github.com/getloopscale/loopzero/pkg/circuitbreaker",
	"github.com/getloopscale/loopzero/pkg/conditions",
	"github.com/getloopscale/loopzero/pkg/enforcement",
	"github.com/getloopscale/loopzero/pkg/errors",
	"github.com/getloopscale/loopzero/pkg/errors/http",
	"github.com/getloopscale/loopzero/pkg/healthcheck",
	"github.com/getloopscale/loopzero/pkg/id",
	"github.com/getloopscale/contracts/jwt",
	"github.com/getloopscale/loopzero/pkg/lock",
	"github.com/getloopscale/loopzero/pkg/logging",
	"github.com/getloopscale/loopzero/pkg/naming",
	"github.com/getloopscale/loopzero/pkg/statemachine",
}

// Default thresholds (bash defaults preserved).
const (
	DefaultMinRequired   = 80.0
	DefaultSlackPP       = 3.0
	CriticalMinRequired  = 90.0
	NewPackageMinFloor   = 50.0
	NewPackageMinCeiling = 90.0
	NewPackageSlackBelow = 5.0 // floor(coverage - 5pp) for new pkgs
)

// PackageBaseline is the per-package coverage row.
//
// JSON wire format (matches bash + the live coverage-baseline.json):
//
//	{ "line": 87.5, "min_required": 80 }
//
// The struct field is named Coverage for Go semantic clarity; the JSON
// tag is "line" (line coverage %, not branch).
type PackageBaseline struct {
	Coverage    float64 `json:"line"`
	MinRequired float64 `json:"min_required"`
}

// Baseline is the full file schema matching infra/quality/coverage-
// baseline.json and the bash baseline-update.sh output:
//
//	{
//	  "schema_version": "1.0",
//	  "generated_at":   "2026-05-21T03:25:37Z",
//	  "critical_packages": [...],
//	  "packages": { "<pkg>": { "line": X.X, "min_required": M } }
//	}
//
// SchemaVersion, GeneratedAt, and CriticalPackagesList are written by
// the baseline-update flow (T5.16); Load() preserves them on
// round-trip. CompareOptions only consults Packages.
type Baseline struct {
	SchemaVersion        string                     `json:"schema_version,omitempty"`
	GeneratedAt          string                     `json:"generated_at,omitempty"`
	CriticalPackagesList []string                   `json:"critical_packages,omitempty"`
	Packages             map[string]PackageBaseline `json:"packages"`
}

// Load reads the baseline JSON file. Returns OPS_USAGE on missing
// file OR malformed JSON.
func Load(path string) (*Baseline, error) {
	body, err := os.ReadFile(path) //nolint:gosec // caller-controlled path
	if err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("baseline: cannot read %s: %v", path, err),
			Cause:   err,
		}
	}
	var b Baseline
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("baseline: %s is not valid JSON: %v", path, err),
			Cause:   err,
		}
	}
	if b.Packages == nil {
		b.Packages = map[string]PackageBaseline{}
	}
	return &b, nil
}

// Marshal renders the baseline exactly as Save writes it: indented JSON with
// packages in alphabetical order (json.Marshal sorts map keys) and a trailing
// newline for POSIX-friendly diffs.
func Marshal(b *Baseline) ([]byte, error) {
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("baseline: json.MarshalIndent failed: %v", err),
			Cause:   err,
		}
	}
	return append(out, '\n'), nil
}

// Save writes the baseline JSON to path with 0o644 perms.
func Save(path string, b *Baseline) error {
	out, err := Marshal(b)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // operator-readable
		return &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("baseline: cannot write %s: %v", path, err),
			Cause:   err,
		}
	}
	return nil
}

// IsCritical returns true if pkg is in the D11 critical-packages list.
func IsCritical(pkg string) bool {
	// Linear scan — list is small (~12 entries). Avoid map allocation.
	for _, c := range CriticalPackages {
		if c == pkg {
			return true
		}
	}
	return false
}

// InitialMinRequired computes the min_required for a newly-discovered
// package per the bash baseline-update rule:
//
//	floor(coverage - 5pp), clamped to [50, 90]
//
// Critical packages always return 90.0 regardless of measured coverage.
func InitialMinRequired(pkg string, coverage float64) float64 {
	if IsCritical(pkg) {
		return CriticalMinRequired
	}
	v := math.Floor(coverage - NewPackageSlackBelow)
	if v < NewPackageMinFloor {
		v = NewPackageMinFloor
	}
	if v > NewPackageMinCeiling {
		v = NewPackageMinCeiling
	}
	return v
}

// PackageNames returns the sorted list of package keys — useful for
// deterministic iteration in compare + update flows.
func (b *Baseline) PackageNames() []string {
	if b == nil || len(b.Packages) == 0 {
		return nil
	}
	names := make([]string, 0, len(b.Packages))
	for k := range b.Packages {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Violation captures a single package that failed the baseline check.
// Drop = min_required - current (positive when below threshold).
type Violation struct {
	Package     string
	Current     float64
	MinRequired float64
	Drop        float64 // min_required - current
}

// CompareOptions controls slack tolerance + default min for new pkgs.
type CompareOptions struct {
	// DefaultMin for packages present in `current` but NOT in `b`
	// (newly added). Default 80.0.
	DefaultMin float64

	// SlackPP — coverage allowed to dip up to this many percentage
	// points BELOW min_required before failing. Default 3.0.
	SlackPP float64
}

// fillCompareDefaults populates zero-value fields with bash defaults.
func (o *CompareOptions) fillCompareDefaults() {
	if o.DefaultMin == 0 {
		o.DefaultMin = DefaultMinRequired
	}
	if o.SlackPP == 0 {
		o.SlackPP = DefaultSlackPP
	}
}

// Compare walks `current.Packages` and asserts each meets its
// effective floor. Returns the list of failing packages sorted
// alphabetically.
//
// Floor logic (matches bash baseline-compare.sh):
//   - Existing package: floor = max(min_required, baseline.line - slack)
//     This means: even if min_required allows lower, the package must
//     not drop more than `slack` pp below its previous baseline (catches
//     gradual regression).
//   - New package: floor = default_min (no slack applied — new packages
//     must meet the absolute floor, not relative).
//
// A package fails when current.Coverage < floor.
func (b *Baseline) Compare(current *Baseline, opts CompareOptions) []Violation {
	opts.fillCompareDefaults()
	if current == nil {
		return nil
	}
	var violations []Violation
	names := current.PackageNames()
	for _, pkg := range names {
		cur := current.Packages[pkg]
		base, isExisting := b.Packages[pkg]

		var floor, minReq float64
		if isExisting {
			minReq = base.MinRequired
			// floor = max(min_required, baseline.line - slack)
			floor = minReq
			if alt := base.Coverage - opts.SlackPP; alt > floor {
				floor = alt
			}
		} else {
			// New package — default_min as absolute floor, no slack.
			minReq = opts.DefaultMin
			floor = opts.DefaultMin
		}

		if cur.Coverage < floor {
			violations = append(violations, Violation{
				Package:     pkg,
				Current:     cur.Coverage,
				MinRequired: minReq,
				Drop:        floor - cur.Coverage,
			})
		}
	}
	return violations
}

// MergeUpdate folds `current` package measurements into `b` per the
// baseline-update rules:
//   - existing package: preserve min_required, update coverage
//   - new package: min_required = InitialMinRequired(pkg, coverage)
//   - critical package: min_required pinned to 90 (never downgraded)
//
// Returns a new Baseline; b is NOT mutated.
func (b *Baseline) MergeUpdate(current *Baseline) *Baseline {
	merged := &Baseline{Packages: map[string]PackageBaseline{}}
	if b != nil {
		for k, v := range b.Packages {
			merged.Packages[k] = v
		}
	}
	if current == nil {
		return merged
	}
	for pkg, cur := range current.Packages {
		existing, present := merged.Packages[pkg]
		if present {
			// Preserve min_required (idempotent); update coverage.
			// Critical pkgs: pin min_required to 90 even if baseline drifted.
			minReq := existing.MinRequired
			if IsCritical(pkg) && minReq < CriticalMinRequired {
				minReq = CriticalMinRequired
			}
			merged.Packages[pkg] = PackageBaseline{
				Coverage:    cur.Coverage,
				MinRequired: minReq,
			}
		} else {
			// New package — seed min_required from coverage.
			merged.Packages[pkg] = PackageBaseline{
				Coverage:    cur.Coverage,
				MinRequired: InitialMinRequired(pkg, cur.Coverage),
			}
		}
	}
	return merged
}
