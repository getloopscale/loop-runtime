// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testpyramid implements the test-pyramid ratio gate (ADR D7):
// unit ≥70%, integration ≤25%, e2e ≤5%. A `pyramid-defer` PR label allows a
// one-PR override.
//
// Plan T5.3 of docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/quality/check-pyramid.sh. Pure-Go: reads the
// test-inventory JSON via encoding/json — ELIMINATES the python3 dependency
// the bash version shelled out to. Genuine pure-logic win per D5.
package testpyramid

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Thresholds are the pyramid bounds (percentages).
type Thresholds struct {
	UnitMinPct        float64
	IntegrationMaxPct float64
	E2EMaxPct         float64
}

// DefaultThresholds per ADR D7.
func DefaultThresholds() Thresholds {
	return Thresholds{UnitMinPct: 70, IntegrationMaxPct: 25, E2EMaxPct: 5}
}

// Config drives the gate.
type Config struct {
	InventoryFile string
	PRLabels      string // comma-separated
	Thresholds    Thresholds
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Deferred   bool
	Violations []string
}

const deferLabel = "pyramid-defer"

type inventory struct {
	Summary struct {
		RatioUnitPct        float64 `json:"ratio_unit_pct"`
		RatioIntegrationPct float64 `json:"ratio_integration_pct"`
		RatioE2EPct         float64 `json:"ratio_e2e_pct"`
	} `json:"summary"`
}

// Check runs the gate.
func Check(cfg Config) (*Outcome, error) {
	// EC-12: pyramid-defer label short-circuits to PASS.
	for _, lbl := range strings.Split(cfg.PRLabels, ",") {
		if strings.TrimSpace(lbl) == deferLabel {
			return &Outcome{Pass: true, Deferred: true}, nil
		}
	}

	data, err := os.ReadFile(cfg.InventoryFile)
	if err != nil {
		return nil, fmt.Errorf("read inventory %s: %w (run test-inventory first)", cfg.InventoryFile, err)
	}
	var inv inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse inventory JSON: %w", err)
	}

	t := cfg.Thresholds
	s := inv.Summary
	outcome := &Outcome{Pass: true}

	if s.RatioUnitPct < t.UnitMinPct {
		outcome.Pass = false
		outcome.Violations = append(outcome.Violations,
			fmt.Sprintf("unit %.1f%% < %.0f%% required", s.RatioUnitPct, t.UnitMinPct))
	}
	if s.RatioIntegrationPct > t.IntegrationMaxPct {
		outcome.Pass = false
		outcome.Violations = append(outcome.Violations,
			fmt.Sprintf("integration %.1f%% > %.0f%% max", s.RatioIntegrationPct, t.IntegrationMaxPct))
	}
	if s.RatioE2EPct > t.E2EMaxPct {
		outcome.Pass = false
		outcome.Violations = append(outcome.Violations,
			fmt.Sprintf("e2e %.1f%% > %.0f%% max", s.RatioE2EPct, t.E2EMaxPct))
	}

	return outcome, nil
}
