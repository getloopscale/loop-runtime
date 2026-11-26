// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testpyramid

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/testinventory"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// # Why the pyramid became DERIVED, and why the verdict is a ratchet
//
// The gate used to read three percentages written in `infra/quality/test-inventory.json`.
// Measured on 2026-08-17: the file listed 710 files, **335 did not exist**, and **649**
// `_test.go` files on disk were not in it — it described 375 of 988, **62%
// blind**, and the 99.4% unit pyramid it declared came from that sample, which excludes
// the entire `infra/tests/e2e` suite.
//
// Derived from the tree, the real pyramid is different:
//
//	              declared     derived
//	files               710         988
//	unit              99.4%       73.5%
//	integration        0.4%       15.7%
//	e2e                0.1%       10.8%
//
// # And here is the decision the measurement forced
//
// The ADR D7 target is `e2e ≤ 5%`. It was chosen when the file claimed 0.1% —
// that is, **calibrated against fiction**. The real number is 10.8%, and 107 e2e files
// in a product whose purpose IS deploying is not obviously a defect.
//
// Making the gate fail today against the ADR target would create a lock the repository
// cannot satisfy, and a lock that cannot turn green is switched off within the
// first week — the same reasoning that refused `HARD` in `B-127` and full enforcement
// in `B-119`.
//
// So the verdict is a **ratchet**: the baseline freezes what was MEASURED, and the gate
// fails when a ratio gets worse. The distance to the ADR target is **reported**, and the
// question "is 10.8% e2e right?" became an item of its own — it belongs to the sponsor, not to me.
//
// This is not "tuning the number until it passes": it separates *not regressing* (verifiable
// today) from *reaching the target* (a decision nobody has made against real data).

// Baseline is the frozen measured pyramid.
type Baseline struct {
	MeasuredAt          string  `json:"measured_at"`
	TotalFiles          int     `json:"total_files"`
	RatioUnitPct        float64 `json:"ratio_unit_pct"`
	RatioIntegrationPct float64 `json:"ratio_integration_pct"`
	RatioE2EPct         float64 `json:"ratio_e2e_pct"`
	Note                string  `json:"note"`
}

// ErrBaselineMissing means the frozen baseline is absent.
//
// Absence is a FAILURE, never a skip: without a baseline the ratchet compares nothing, and a
// gate that compares nothing while reporting green is the defect this item removed.
var ErrBaselineMissing = errors.New("pyramid baseline missing — without it the ratchet compares nothing, and a verdict about nothing is not a verdict (run with --write)")

// ErrRegressed means a ratio moved the wrong way past the baseline.
var ErrRegressed = errors.New("the pyramid regressed relative to the measured baseline")

// tolerancePct absorbs the noise of a file coming in or going out. With ~988 files,
// one file is worth ~0.1 point; 0.5 leaves room for half a dozen without letting a
// change of shape through.
const tolerancePct = 0.5

// DerivedOutcome reports both verdicts: what blocks and what is merely reported.
type DerivedOutcome struct {
	Total    int
	UnitPct  float64
	IntegPct float64
	E2EPct   float64
	// Regressions block.
	Regressions []string
	// AspirationGaps are the distance to the ADR target — reported, not blocking,
	// because the target was never measured against the real tree.
	AspirationGaps []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o DerivedOutcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// Pass reports whether nothing regressed.
func (o DerivedOutcome) Pass() bool { return len(o.Regressions) == 0 }

// LoadBaseline reads the frozen measurement.
func LoadBaseline(path string) (Baseline, error) {
	var b Baseline
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return b, fmt.Errorf("%w: %s", ErrBaselineMissing, path)
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return b, nil
}

// WriteBaseline freezes the current measurement.
func WriteBaseline(path string, inv testinventory.Inventory, measuredAt, note string) error {
	u, i, e := inv.Ratios()
	b := Baseline{
		MeasuredAt:          measuredAt,
		TotalFiles:          inv.Total(),
		RatioUnitPct:        round1(u),
		RatioIntegrationPct: round1(i),
		RatioE2EPct:         round1(e),
		Note:                note,
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// DeferLabel is the PR label that defers the ratchet for a single PR.
const DeferLabel = "pyramid-defer"

// Deferred reports a run the DeferLabel excused from measuring.
type Deferred struct{}

// Verdict is Skip: a deferred run measured nothing, and that is the one case
// where not measuring is the right answer.
func (Deferred) Verdict() verdict.Verdict { return verdict.Skip }

// Deferral returns a Deferred when labels (comma-separated, as PR_LABELS
// carries them) include DeferLabel.
func Deferral(labels string) (Deferred, bool) {
	for _, lbl := range strings.Split(labels, ",") {
		if strings.TrimSpace(lbl) == DeferLabel {
			return Deferred{}, true
		}
	}
	return Deferred{}, false
}

// Refrozen reports a baseline frozen from the current tree.
type Refrozen struct {
	Total                     int
	UnitPct, IntegPct, E2EPct float64
}

// Verdict is Pass: freezing is an operation, and a Refrozen exists only when
// the baseline was written.
func (Refrozen) Verdict() verdict.Verdict { return verdict.Pass }

// Refreeze derives the pyramid from the tree under repoRoot and freezes it at
// baselinePath, dated measuredAt (YYYY-MM-DD).
func Refreeze(repoRoot, baselinePath, measuredAt string) (Refrozen, error) {
	derived, err := testinventory.Derive(repoRoot)
	if err != nil {
		return Refrozen{}, err
	}
	note := "Derived from the tree by testinventory.Derive, not declared (B-106)."
	if err := WriteBaseline(baselinePath, derived, measuredAt, note); err != nil {
		return Refrozen{}, err
	}
	u, i, e := derived.Ratios()
	return Refrozen{Total: derived.Total(), UnitPct: u, IntegPct: i, E2EPct: e}, nil
}

// CheckDerived derives the pyramid from the tree and ratchets it against the frozen
// baseline, reporting the distance to the ADR aspiration separately.
func CheckDerived(repoRoot, baselinePath string, thresholds Thresholds) (DerivedOutcome, error) {
	var out DerivedOutcome

	inv, err := testinventory.Derive(repoRoot)
	if err != nil {
		return out, err
	}
	base, err := LoadBaseline(baselinePath)
	if err != nil {
		return out, err
	}

	u, i, e := inv.Ratios()
	out.Total, out.UnitPct, out.IntegPct, out.E2EPct = inv.Total(), round1(u), round1(i), round1(e)

	// Ratchet: unit may only go UP, integration and e2e may only go DOWN.
	if out.UnitPct < base.RatioUnitPct-tolerancePct {
		out.Regressions = append(out.Regressions, fmt.Sprintf(
			"unit fell from %.1f%% (baseline) to %.1f%%", base.RatioUnitPct, out.UnitPct))
	}
	if out.IntegPct > base.RatioIntegrationPct+tolerancePct {
		out.Regressions = append(out.Regressions, fmt.Sprintf(
			"integration rose from %.1f%% to %.1f%%", base.RatioIntegrationPct, out.IntegPct))
	}
	if out.E2EPct > base.RatioE2EPct+tolerancePct {
		out.Regressions = append(out.Regressions, fmt.Sprintf(
			"e2e rose from %.1f%% to %.1f%%", base.RatioE2EPct, out.E2EPct))
	}

	// The distance to the ADR target — reported, never blocking while the target has not
	// been measured against the real tree.
	if out.UnitPct < thresholds.UnitMinPct {
		out.AspirationGaps = append(out.AspirationGaps, fmt.Sprintf(
			"unit %.1f%% < target %.1f%% (ADR D7)", out.UnitPct, thresholds.UnitMinPct))
	}
	if out.IntegPct > thresholds.IntegrationMaxPct {
		out.AspirationGaps = append(out.AspirationGaps, fmt.Sprintf(
			"integration %.1f%% > target %.1f%% (ADR D7)", out.IntegPct, thresholds.IntegrationMaxPct))
	}
	// e2e RATIFIED by the ADR D7 amendment on 2026-09-06 (Option A of B-137):
	// docs/decisions.md. For a
	// product whose purpose IS deploying, ~9% e2e is genuine coverage — the ≤5% target
	// was calibrated against fiction (0.1% declared vs 10.8% real) and the ratchet is
	// already the canonical enforcement mechanism (trajectory 10.8→9.6% in 20d with no
	// intervention). The e2e gap is no longer reported; the ratchet stays active.

	if !out.Pass() {
		out.Violation = ErrRegressed
		return out, nil
	}
	return out, nil
}
