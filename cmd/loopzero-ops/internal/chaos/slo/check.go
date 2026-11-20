// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package slo is the pure-logic SLO verdict of the chaos resilience runner.
//
// It was called `chaosslo` and lived in internal/quality/, where it did not belong:
// it serves the `chaos` subcommand, not a gate, so the `quality --list` oracle
// never enumerated it (B-066). The name prefix disambiguated it inside that
// directory; under internal/chaos/ the path says the same.
// (Track C TC3.1 of 2026-05-24-drill-consolidation-migration-plan).
//
// It compares baseline / during / post chaos samples and decides whether the
// SLO held (PASS) or was breached (FAIL), or whether a required metric is
// missing (INCONCLUSIVE). This is the pure arithmetic extracted from
// infra/scripts/chaos/run-scenario.sh:230-259 — the chaos-mesh injection
// (kubectl apply) and the LIVE sampling (baseline.sh curl/kubectl) stay bash
// (orchestration, ADR D1/D4).
//
// Faithful port of run-scenario.sh thresholds (lines 231-233):
//   - API success during chaos >= 90%
//   - p95 degradation during chaos <= 500ms over baseline
//   - post-recovery p95 drift <= 100ms over baseline
//   - loopzero-api restart delta <= 2
package slo

import (
	"encoding/json"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/exitcode"
)

// Thresholds mirror run-scenario.sh:231-233 + the post-drift 100ms (line 250).
type Thresholds struct {
	SuccessRatePct       int // during-chaos API success floor (default 90)
	LatencyDegradationMs int // during-chaos p95 ceiling over baseline (default 500)
	PostDriftMs          int // post-recovery p95 drift ceiling over baseline (default 100)
	RestartDelta         int // loopzero-api restart delta ceiling (default 2)
}

// DefaultThresholds returns the run-scenario.sh defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{SuccessRatePct: 90, LatencyDegradationMs: 500, PostDriftMs: 100, RestartDelta: 2}
}

// Sample is the subset of a baseline.json / during.json / post.json consumed by
// the verdict (the jq paths used in run-scenario.sh:137-210). Pointers
// distinguish "missing" (nil → INCONCLUSIVE) from a real zero value.
type Sample struct {
	P95ms               *int
	SuccessPct          *int
	LoopZeroAPIRestarts *int
}

type rawSample struct {
	APIHealth struct {
		LatencyP95ms *int `json:"latency_p95_ms"`
		SuccessRate  *int `json:"success_rate_pct"`
	} `json:"api_health"`
	PodRestartCounts struct {
		LoopZeroAPI *int `json:"loopzero_api"`
	} `json:"pod_restart_counts"`
}

// ParseSample decodes the relevant fields from a baseline-style JSON document.
func ParseSample(data []byte) (Sample, error) {
	var r rawSample
	if err := json.Unmarshal(data, &r); err != nil {
		return Sample{}, err
	}
	return Sample{
		P95ms:               r.APIHealth.LatencyP95ms,
		SuccessPct:          r.APIHealth.SuccessRate,
		LoopZeroAPIRestarts: r.PodRestartCounts.LoopZeroAPI,
	}, nil
}

// Result is the verdict category.
type Result int

const (
	// Pass — SLO held during chaos AND recovered within budget.
	Pass Result = iota
	// Fail — an SLO threshold was breached.
	Fail
	// Inconclusive — a required metric was missing (re-run; not a failure).
	Inconclusive
)

func (r Result) String() string {
	switch r {
	case Pass:
		return "PASS"
	case Fail:
		return "FAIL"
	default:
		return "INCONCLUSIVE"
	}
}

// ExitCode maps the verdict onto loopzero-ops' exit table (internal/exitcode): a
// breached SLO is a failure, and an inconclusive run is incomplete.
func (r Result) ExitCode() int {
	switch r {
	case Pass:
		return exitcode.OK
	case Fail:
		return exitcode.Failure
	default:
		return exitcode.Incomplete
	}
}

// Verdict is the result plus the human-readable breach reasons.
type Verdict struct {
	Result  Result
	Reasons []string
}

// Compare runs the verdict over the three samples. Mirrors
// run-scenario.sh:235-259 exactly: start PASS, append a reason + flip to FAIL
// for each breached threshold. If any required field is missing → INCONCLUSIVE.
func Compare(pre, during, post Sample, thr Thresholds) Verdict {
	// INCONCLUSIVE guard: every field the verdict reads must be present.
	if pre.P95ms == nil || during.P95ms == nil || post.P95ms == nil ||
		during.SuccessPct == nil || pre.LoopZeroAPIRestarts == nil || post.LoopZeroAPIRestarts == nil {
		return Verdict{Result: Inconclusive, Reasons: []string{"missing required metric in baseline/during/post sample"}}
	}

	v := Verdict{Result: Pass}

	if *during.SuccessPct < thr.SuccessRatePct {
		v.Result = Fail
		v.Reasons = append(v.Reasons, fmt.Sprintf("API success during chaos: %d%% < %d%%", *during.SuccessPct, thr.SuccessRatePct))
	}
	if deg := *during.P95ms - *pre.P95ms; deg > thr.LatencyDegradationMs {
		v.Result = Fail
		v.Reasons = append(v.Reasons, fmt.Sprintf("P95 degradation during chaos: %dms > %dms", deg, thr.LatencyDegradationMs))
	}
	if drift := *post.P95ms - *pre.P95ms; drift > thr.PostDriftMs {
		v.Result = Fail
		v.Reasons = append(v.Reasons, fmt.Sprintf("Post-recovery latency drift: %dms > %dms", drift, thr.PostDriftMs))
	}
	if delta := *post.LoopZeroAPIRestarts - *pre.LoopZeroAPIRestarts; delta > thr.RestartDelta {
		v.Result = Fail
		v.Reasons = append(v.Reasons, fmt.Sprintf("loopzero-api restart delta: %d > %d", delta, thr.RestartDelta))
	}
	return v
}
