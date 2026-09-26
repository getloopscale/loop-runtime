// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package baselineupdate orchestrates the post-merge / cron baseline
// refresh per Phase 5 T5.16 of plan
// 2026-05-21-bash-to-go-migration-plan. Replaces
// infra/scripts/quality/baseline-update.sh (285 LOC) — the biggest
// individual Phase 5 script.
//
// Pipeline:
//  1. Parse Go coverage profile (if present) → per-pkg %
//  2. Parse CLI lcov (if present)            → per-pkg %
//  3. Load existing baseline (if present, optional)
//  4. MergeUpdate (preserve min_required, pin critical pkgs to 90)
//  5. Stamp SchemaVersion + GeneratedAt + CriticalPackagesList
//  6. Save (or print to stdout when DryRun)
package baselineupdate

import (
	"fmt"
	"os"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baseline"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/coverageparse"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

const schemaVersion = "1.0"

// Config holds the inputs.
type Config struct {
	// GoProfilePath — `go test -coverprofile=PATH` output. Empty OR
	// missing file → silently skipped (matches bash semantic).
	GoProfilePath string

	// LCOVPath — lcov.info for CLI. Same skip-on-empty semantic.
	LCOVPath string

	// BaselinePath — checked-in baseline JSON. Empty existing file →
	// fresh baseline. Required (write target).
	BaselinePath string

	// DryRun: print the would-be JSON to stdout instead of writing.
	DryRun bool

	// Clock injects deterministic timestamps for tests. Default time.Now.
	Clock func() time.Time
}

// Result captures the outcome of an update run.
type Result struct {
	// PackagesDiscovered is the count of pkgs found across go + lcov
	// inputs (before merge with existing).
	PackagesDiscovered int

	// PackagesWritten is the final count in the merged baseline.
	PackagesWritten int

	// OutputPath is where the file was written (DryRun=false) or "stdout".
	OutputPath string

	// JSON is populated only when DryRun=true (the would-be content).
	JSON []byte
}

// Verdict is Pass: the update is an operation, and a Result exists only when
// it completed. A failed update is Run's error.
func (r Result) Verdict() verdict.Verdict { return verdict.Pass }

// Run executes the pipeline.
//
// Errors:
//
//	OPS_USAGE          — missing BaselinePath / unreadable file
//	OPS_DEPLOY_FAILURE — parse failure / write failure
//	OPS_PARTIAL        — both profiles absent (nothing to update)
func Run(cfg Config) (*Result, error) {
	if cfg.BaselinePath == "" {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: "baselineupdate: BaselinePath required",
		}
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}

	// Step 1+2: collect per-package coverage from both sources.
	current := &baseline.Baseline{Packages: map[string]baseline.PackageBaseline{}}
	totalDiscovered := 0

	if cfg.GoProfilePath != "" {
		if pkgs, err := readGoProfile(cfg.GoProfilePath); err != nil {
			return nil, err
		} else {
			for _, p := range pkgs {
				current.Packages[p.Package] = baseline.PackageBaseline{Coverage: p.Coverage}
				totalDiscovered++
			}
		}
	}

	if cfg.LCOVPath != "" {
		if pkgs, err := readLCOV(cfg.LCOVPath); err != nil {
			return nil, err
		} else {
			for _, p := range pkgs {
				current.Packages[p.Package] = baseline.PackageBaseline{Coverage: p.Coverage}
				totalDiscovered++
			}
		}
	}

	if totalDiscovered == 0 {
		return nil, &loopzeroerrors.LoopZeroError{
			Code: "OPS_PARTIAL",
			Message: "baselineupdate: no packages discovered (both Go profile + lcov empty/missing) — " +
				"nothing to update",
		}
	}

	// Step 3+4: load existing baseline (optional) + merge.
	var existing *baseline.Baseline
	if _, err := os.Stat(cfg.BaselinePath); err == nil {
		existing, err = baseline.Load(cfg.BaselinePath)
		if err != nil {
			return nil, err
		}
	} else {
		existing = &baseline.Baseline{Packages: map[string]baseline.PackageBaseline{}}
	}
	merged := existing.MergeUpdate(current)

	// Step 5: stamp metadata.
	merged.SchemaVersion = schemaVersion
	merged.GeneratedAt = cfg.Clock().UTC().Format(time.RFC3339)
	merged.CriticalPackagesList = append([]string(nil), baseline.CriticalPackages...)

	// Step 6: persist OR return JSON.
	result := &Result{
		PackagesDiscovered: totalDiscovered,
		PackagesWritten:    len(merged.Packages),
	}

	if cfg.DryRun {
		body, err := baseline.Marshal(merged)
		if err != nil {
			return nil, err
		}
		result.JSON = body
		result.OutputPath = "stdout"
		return result, nil
	}

	if err := baseline.Save(cfg.BaselinePath, merged); err != nil {
		return nil, err
	}
	result.OutputPath = cfg.BaselinePath
	return result, nil
}

// readGoProfile opens + parses a Go coverage profile. Missing file →
// nil slice + nil error (silent skip per bash semantic).
func readGoProfile(path string) ([]coverageparse.PackageCoverage, error) {
	f, err := os.Open(path) //nolint:gosec // operator-controlled path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("baselineupdate: cannot open Go profile %s: %v", path, err),
			Cause:   err,
		}
	}
	defer func() { _ = f.Close() }()
	return coverageparse.ParseGoProfile(f)
}

// readLCOV opens + parses an lcov.info. Missing file → silent skip.
func readLCOV(path string) ([]coverageparse.PackageCoverage, error) {
	f, err := os.Open(path) //nolint:gosec // operator-controlled path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("baselineupdate: cannot open lcov %s: %v", path, err),
			Cause:   err,
		}
	}
	defer func() { _ = f.Close() }()
	return coverageparse.ParseLCOV(f)
}
