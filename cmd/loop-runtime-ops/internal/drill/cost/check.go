// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package cost is the pure-logic core of infra/scripts/drill/cost-report.sh.
//
// It was called `drillcost` and lived in internal/quality/, where it did not belong:
// it serves the `drill` subcommand, not a gate, so the `quality --list` oracle
// never enumerated it (B-066). The name prefix disambiguated it inside that
// directory; under internal/drill/ the path says the same.
// (Track C TC2.1 of 2026-05-24-drill-consolidation-migration-plan).
//
// It computes a drill run's cost from a provision/teardown window + DO hourly
// rates, and renders the markdown report. This is the ARITHMETIC + FORMATTING
// half — the `--auto` Pulumi-state read (`npx pulumi stack output`) that
// discovers the window stays in bash (orchestration, ADR D1), and pipes the
// discovered --start/--end into `loop-runtime-ops drill cost`.
//
// Faithful port: the default rates and the markdown table mirror
// cost-report.sh:39-45,130-151 exactly so the bash --auto wrapper produces an
// identical report when it delegates here.
package cost

import (
	"fmt"
	"strings"
	"time"
)

// Rates are the DO hourly rates ($/hr) for the drill stack (cost-report.sh:39-45).
type Rates struct {
	Mgmt     float64
	Build    float64
	Runtime  float64
	Runtime2 float64
	PG       float64
	Total    float64
}

// DefaultRates mirror cost-report.sh:40-45 (DO public catalog, 2026-05-16).
func DefaultRates() Rates {
	return Rates{
		Mgmt:     0.214,
		Build:    0.071,
		Runtime:  0.071,
		Runtime2: 0.071,
		PG:       0.125,
		Total:    0.552,
	}
}

// Report is the computed cost breakdown.
type Report struct {
	StartISO        string
	EndISO          string
	DurationSeconds int64
	DurationMinutes int64
	DurationHours   float64
	CostMgmt        float64
	CostBuild       float64
	CostRuntime     float64
	CostRuntime2    float64
	CostPG          float64
	CostTotal       float64
}

// isoLayouts are the timestamp layouts accepted (RFC3339 + the date -u -d form).
var isoLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05Z07:00",
}

func parseISO(s string) (time.Time, error) {
	for _, l := range isoLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid ISO timestamp: %q", s)
}

// Compute validates the window and computes the cost breakdown. It mirrors
// cost-report.sh:106-127: both timestamps must parse, end must be strictly
// after start, cost = rate * hours.
func Compute(startISO, endISO string, r Rates) (Report, error) {
	start, err := parseISO(startISO)
	if err != nil {
		return Report{}, fmt.Errorf("start: %w", err)
	}
	end, err := parseISO(endISO)
	if err != nil {
		return Report{}, fmt.Errorf("end: %w", err)
	}
	if !end.After(start) {
		return Report{}, fmt.Errorf("end (%s) must be after start (%s)", endISO, startISO)
	}
	secs := int64(end.Sub(start).Seconds())
	hours := float64(secs) / 3600.0
	round2 := func(rate float64) float64 {
		// match python f'{x:.2f}' two-decimal rounding
		return float64(int64(rate*hours*100+0.5)) / 100
	}
	return Report{
		StartISO:        startISO,
		EndISO:          endISO,
		DurationSeconds: secs,
		DurationMinutes: secs / 60,
		DurationHours:   hours,
		CostMgmt:        round2(r.Mgmt),
		CostBuild:       round2(r.Build),
		CostRuntime:     round2(r.Runtime),
		CostRuntime2:    round2(r.Runtime2),
		CostPG:          round2(r.PG),
		CostTotal:       round2(r.Total),
	}, nil
}

// Markdown renders the report section, mirroring cost-report.sh:130-151.
func (rep Report) Markdown(r Rates) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Drill Run Cost Report\n\n")
	fmt.Fprintf(&b, "**Window:** `%s` → `%s`  (%d min / %.2f h)\n\n", rep.StartISO, rep.EndISO, rep.DurationMinutes, rep.DurationHours)
	fmt.Fprintf(&b, "| Resource | Rate ($/hr) | Cost |\n")
	fmt.Fprintf(&b, "|---|---|---|\n")
	fmt.Fprintf(&b, "| mgmt cluster (3 × s-4vcpu-8gb-amd) | $%g | $%.2f |\n", r.Mgmt, rep.CostMgmt)
	fmt.Fprintf(&b, "| build cluster (2 × s-2vcpu-4gb) | $%g | $%.2f |\n", r.Build, rep.CostBuild)
	fmt.Fprintf(&b, "| runtime cluster (2 × s-2vcpu-4gb) | $%g | $%.2f |\n", r.Runtime, rep.CostRuntime)
	fmt.Fprintf(&b, "| runtime-2 cluster (2 × s-2vcpu-4gb) | $%g | $%.2f |\n", r.Runtime2, rep.CostRuntime2)
	fmt.Fprintf(&b, "| managed PG (db-s-2vcpu-4gb) | $%g | $%.2f |\n", r.PG, rep.CostPG)
	fmt.Fprintf(&b, "| Spaces (3 buckets, baseline) | ~$0.007 | included |\n")
	fmt.Fprintf(&b, "| **Total** | **$%g/hr** | **$%.2f** |\n\n", r.Total, rep.CostTotal)
	fmt.Fprintf(&b, "**Pricing source:** DO public catalog, 2026-05-16.\n\n")
	fmt.Fprintf(&b, "**Notes:**\n")
	fmt.Fprintf(&b, "- Spaces buckets billed at $5/mo baseline; ephemeral run cost negligible.\n")
	fmt.Fprintf(&b, "- Plan 2026-05-16 budget envelope: ~$2,000-3,000/yr cadence completa\n")
	fmt.Fprintf(&b, "  (52 weekly × Stage 1+L2 + 52 weekly × Stage 2 + 4 quarterly DR + L3/L4 ad-hoc).\n")
	return b.String()
}
