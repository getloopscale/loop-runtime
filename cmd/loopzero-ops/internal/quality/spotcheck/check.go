// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package spotcheck replaces infra/scripts/governance/spotcheck-dimension.sh:
// pick 1 dimension + 1 persona (seeded-deterministic) for the nightly
// adversarial spotcheck, and emit the grepable lines + JSON the workflow
// consumes.
//
// Note: the Go RNG mapping (math/rand seeded) differs from bash's $RANDOM
// sequence — a given seed selects a different pair than the old bash. The
// contract preserved is (a) deterministic-per-seed and (b) the
// `selected_dimension:` / `selected_persona:` grepable output lines that
// adversarial-spotcheck.yml parses. No caller depends on a specific
// seed→selection mapping (the seed is a debug-repro aid).
package spotcheck

import (
	"fmt"
	"math/rand"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// Dimensions are the audit dimensions the spotcheck samples from.
var Dimensions = []string{
	"core-deploy", "dx-cli", "infra-ops", "security",
	"scale-mt", "product-completeness", "tests-confidence",
}

// Personas are the adversarial reviewer personas.
var Personas = []string{"p1_vercel_dd", "p2_hostile_vc", "p3_paranoid_sre"}

// Config controls the selection.
type Config struct {
	// Seed makes the selection deterministic. Callers pass time.Now().Unix()
	// for a fresh pick or a fixed value for reproducibility.
	Seed int64
}

// Outcome is the selection result.
type Outcome struct {
	Seed      int64
	Dimension string
	Persona   string
	Method    string
	// Messages is the rendered output (grepable lines + JSON block) that the
	// CLI runner prints to stdout.
	Messages []string
}

// Verdict is Pass: a spot check selects what to audit next and judges
// nothing, so an Outcome exists only when the selection was made.
func (o Outcome) Verdict() verdict.Verdict { return verdict.Pass }

// summaryFile is the 2026-05-11 audit baseline. It lived under the gitignored
// .claude/knowledge-base/ and was deleted in f082f6ecd, so the only copy a
// reader can open is the one in history.
const summaryFile = "`git show f082f6ecd~1:.claude/knowledge-base/audits/2026-05-11/SUMMARY.md`"

// Run performs the seeded selection and renders the output contract.
func Run(cfg Config) (*Outcome, error) {
	// #nosec G404 -- non-crypto: a seeded, reproducible audit-dimension pick.
	r := rand.New(rand.NewSource(cfg.Seed))
	dim := Dimensions[r.Intn(len(Dimensions))]
	persona := Personas[r.Intn(len(Personas))]
	method := "seeded_uniform"

	out := &Outcome{Seed: cfg.Seed, Dimension: dim, Persona: persona, Method: method}
	out.Messages = []string{
		fmt.Sprintf("spotcheck_seed: %d", cfg.Seed),
		fmt.Sprintf("selected_dimension: %s", dim),
		fmt.Sprintf("selected_persona: %s", persona),
		fmt.Sprintf("selection_method: %s (improvement: weight by ttl_remaining)", method),
		"{",
		fmt.Sprintf("  %q: %d,", "seed", cfg.Seed),
		fmt.Sprintf("  %q: %q,", "dimension", dim),
		fmt.Sprintf("  %q: %q,", "persona", persona),
		fmt.Sprintf("  %q: %q,", "selection_method", method),
		fmt.Sprintf("  %q: %q", "next_step",
			"Invoke persona agent with this dimension; compare against "+summaryFile+"; alert if delta > 0.3"),
		"}",
	}
	return out, nil
}
