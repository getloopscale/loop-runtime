// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package baselinecompare implements the baseline-compare quality gate
// per ADR T2.2 + EC-2 escape valve. Replaces
// infra/scripts/quality/baseline-compare.sh (~109 LOC) per Phase 5
// T5.12 of plan
// 2026-05-21-bash-to-go-migration-plan.
//
// Origin: plan 2026-05-21-comprehensive-quality-gates v1.1 T2.2.
//
// Gate logic (matches bash):
//   - Load checked-in baseline JSON + current measurement JSON.
//   - For each pkg in `current`:
//   - If pkg in baseline: floor = max(min_required, baseline.line - slack)
//   - If pkg NEW:         floor = default_min (no slack)
//   - Fail when current.line < floor.
//
// EC-2 escape valve: PR can carry the `coverage-baseline-defer` label.
// When present AND a non-empty reason is supplied (DEFER_REASON env or
// caller-passed Reason), the gate emits PASS with a deferred-record
// note. Label-without-reason = HARD FAIL (guards against blank label
// approval).
package baselinecompare

import (
	"fmt"
	"io"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/baseline"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// Config holds the gate inputs.
type Config struct {
	// BaselinePath is the checked-in baseline JSON.
	// Required.
	BaselinePath string

	// CurrentPath is the PR-side coverage measurement JSON.
	// Required.
	CurrentPath string

	// DefaultMin for newly-added packages (no baseline entry).
	// Default 80.0.
	DefaultMin float64

	// SlackPP — tolerance below baseline.line (pp).
	// Default 3.0.
	SlackPP float64

	// DeferLabelPresent is true when the PR carries the
	// `coverage-baseline-defer` label.
	DeferLabelPresent bool

	// DeferReason is the operator-supplied justification (e.g. from
	// PR body "Defer reason: ..."). Required when DeferLabelPresent.
	DeferReason string
}

// Outcome captures the gate result + diagnostic message stream.
type Outcome struct {
	// Pass is true when the gate passes (or is deferred via EC-2).
	Pass bool

	// Deferred is true when EC-2 escape valve was taken.
	Deferred bool

	// Violations is non-empty when Pass=false from coverage regression
	// (vs deferred OR config error).
	Violations []baseline.Violation

	// Messages collects log lines for the operator (stdout-bound when
	// Pass=true, stderr-bound when Pass=false). Order-preserving.
	Messages []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Run executes the gate. Returns (*Outcome, error). Outcome.Pass
// drives the caller's exit code; non-nil error means the gate itself
// could not run (e.g., missing file).
//
// Errors:
//
//	OPS_USAGE — config violations (DeferLabel without DeferReason,
//	            missing required path, malformed JSON)
func Run(cfg Config) (*Outcome, error) {
	// EC-2 escape valve first — short-circuits even if files are missing.
	if cfg.DeferLabelPresent {
		if cfg.DeferReason == "" {
			return nil, &loopruntimeerrors.LoopRuntimeError{
				Code: "OPS_USAGE",
				Message: "baseline-compare: EC-2 guard — `coverage-baseline-defer` label present " +
					"but DeferReason empty; add 'Defer reason: <explanation>' to PR body",
			}
		}
		return &Outcome{
			Pass:     true,
			Deferred: true,
			Messages: []string{
				"[baseline-compare] EC-2: coverage-baseline-defer label active",
				"[baseline-compare] reason: " + cfg.DeferReason,
				"[baseline-compare] PASS — gate deferred per EC-2 (7-day TTL applies)",
				"[baseline-compare] NOTE: bot will open follow-up issue for coverage push",
			},
		}, nil
	}

	if cfg.BaselinePath == "" {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: "baseline-compare: BaselinePath required",
		}
	}
	if cfg.CurrentPath == "" {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: "baseline-compare: CurrentPath required",
		}
	}

	base, err := baseline.Load(cfg.BaselinePath)
	if err != nil {
		return nil, err
	}
	current, err := baseline.Load(cfg.CurrentPath)
	if err != nil {
		return nil, err
	}

	opts := baseline.CompareOptions{DefaultMin: cfg.DefaultMin, SlackPP: cfg.SlackPP}
	violations := base.Compare(current, opts)

	out := &Outcome{Violations: violations}
	if len(violations) == 0 {
		out.Pass = true
		out.Messages = []string{
			fmt.Sprintf(
				"[baseline-compare] PASS — all %d packages above floor (slack=%.1fpp)",
				len(current.Packages), resolveSlack(opts),
			),
		}
		return out, nil
	}

	// Fail path — format diagnostic lines mirroring bash output.
	out.Pass = false
	out.Messages = append(out.Messages, fmt.Sprintf(
		"[baseline-compare] FAIL — %d package(s) below floor:", len(violations)))
	for _, v := range violations {
		floor := v.Current + v.Drop
		// "  PKG: current=X.X% < floor=F.F% (min_required=M, baseline=B)"
		baseLine := "N/A"
		if base.Packages != nil {
			if b, ok := base.Packages[v.Package]; ok {
				baseLine = fmt.Sprintf("%.1f", b.Coverage)
			}
		}
		note := ""
		if _, existed := base.Packages[v.Package]; !existed {
			note = " (NEW package, no baseline)"
		}
		out.Messages = append(out.Messages, fmt.Sprintf(
			"  %s: current=%.1f%% < floor=%.1f%% (min_required=%.0f, baseline=%s)%s",
			v.Package, v.Current, floor, v.MinRequired, baseLine, note,
		))
	}
	return out, nil
}

// resolveSlack returns the effective SlackPP (default 3.0 when zero).
// Centralized so the message reflects the actual slack applied.
func resolveSlack(opts baseline.CompareOptions) float64 {
	if opts.SlackPP == 0 {
		return baseline.DefaultSlackPP
	}
	return opts.SlackPP
}

// WriteMessages emits the Outcome.Messages to the given writer, one
// per line. Convenience for callers that just want to flush the
// gate's stdout/stderr.
func (o *Outcome) WriteMessages(w io.Writer) error {
	for _, m := range o.Messages {
		if _, err := fmt.Fprintln(w, m); err != nil {
			return err
		}
	}
	return nil
}
