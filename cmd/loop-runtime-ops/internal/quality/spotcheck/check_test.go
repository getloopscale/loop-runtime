// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package spotcheck

import (
	"slices"
	"strings"
	"testing"
)

func TestRun_Deterministic_SameSeedSameResult(t *testing.T) {
	a, err := Run(Config{Seed: 42})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := Run(Config{Seed: 42})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.Dimension != b.Dimension || a.Persona != b.Persona {
		t.Fatalf("same seed must give same selection: %q/%q vs %q/%q",
			a.Dimension, a.Persona, b.Dimension, b.Persona)
	}
}

func TestRun_SelectionWithinAllowedSets(t *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		out, err := Run(Config{Seed: seed})
		if err != nil {
			t.Fatalf("Run seed=%d: %v", seed, err)
		}
		if !slices.Contains(Dimensions, out.Dimension) {
			t.Fatalf("seed=%d: dimension %q not in allowed set", seed, out.Dimension)
		}
		if !slices.Contains(Personas, out.Persona) {
			t.Fatalf("seed=%d: persona %q not in allowed set", seed, out.Persona)
		}
	}
}

func TestRun_OutputContract_HasGrepableLines(t *testing.T) {
	// The adversarial-spotcheck.yml caller greps `^selected_dimension:` and
	// `^selected_persona:` then awk '{print $2}'. The rendered output MUST
	// preserve those lines verbatim.
	out, _ := Run(Config{Seed: 7})
	text := strings.Join(out.Messages, "\n")
	if !strings.Contains(text, "selected_dimension: "+out.Dimension) {
		t.Fatalf("missing grepable `selected_dimension: %s` line in:\n%s", out.Dimension, text)
	}
	if !strings.Contains(text, "selected_persona: "+out.Persona) {
		t.Fatalf("missing grepable `selected_persona: %s` line in:\n%s", out.Persona, text)
	}
	if !strings.Contains(text, "spotcheck_seed: 7") {
		t.Fatalf("missing `spotcheck_seed: 7` line")
	}
}

func TestRun_DistributionCoversMultipleDimensions(t *testing.T) {
	// Sanity: across 100 seeds the selection is not stuck on one dimension.
	seen := map[string]bool{}
	for seed := int64(0); seed < 100; seed++ {
		out, _ := Run(Config{Seed: seed})
		seen[out.Dimension] = true
	}
	if len(seen) < 2 {
		t.Fatalf("selection appears stuck — only %d distinct dimension(s) over 100 seeds", len(seen))
	}
}

// The baseline the next step compares against must be openable by whoever
// reads the output: a path under the gitignored kit resolves for nobody.
func TestRun_NextStepPointsAtAnOpenableBaseline(t *testing.T) {
	out, err := Run(Config{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out.Messages, "\n")
	if !strings.Contains(joined, "git show f082f6ecd~1:") {
		t.Fatalf("next_step does not point at the baseline's surviving copy:\n%s", joined)
	}
}
