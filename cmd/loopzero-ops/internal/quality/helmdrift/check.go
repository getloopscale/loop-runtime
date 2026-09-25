// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package helmdrift detects drift between published charts (charts/) and
// bootstrap charts (infra/helm/) by comparing template-file sets. A drift is
// acceptable only if it is recorded in infra/quality/helm-drift-registry.yaml,
// citing a row of docs/decisions.md (per ADR-0041, gate CH-G16).
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/audit-helm-drift.sh. Pure-Go (os.ReadDir + set diff
// + static registry) — no external CLI (bash used find/comm/awk). Genuine
// pure-logic win per D5.
//
// # Two comparisons, one gate (B-001)
//
// The gate asks two questions of each pair and reports them apart:
//
//	KINDS  — do the two sides install the same TYPES of resource?
//	RBAC   — do the two sides GRANT the same (apiGroup, resource, verb) cells?
//
// The second was added because the first cannot see a grant divergence at all:
// both sides of the loopzero-api pair install exactly one ClusterRole, so the pair
// was green while it diverged on 174 cells of a 200-cell union.
//
// # Which pairs
//
// The pairs are read from infra/quality/chart-pairs.txt through
// chartpairs.LoadRegistry — the same registry the chart-pairs gate keeps
// honest against disk — so this gate, chart-pairs and helm-overlap-parity
// audit one list. Until 2026-09-23 this gate discovered pairs from disk plus a
// hard-coded map of differently-named pairs, and the three disagreed.
package helmdrift

import (
	"os"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
)

// # Why the kind registry is keyed by KIND and not by file name
//
// Until 2026-08-16 the key was the template file name, and 13 of the 21 entries
// described something that was not a divergence: `service.yaml` on one side
// against `svc.yaml` on the other, or a conditional file that NEITHER side renders
// under default values. Each of those entries was a line someone read as
// "accepted divergence", and an inflated allowlist is an allowlist nobody reviews
// — precisely where the real ones live. Comparing what the charts INSTALL leaves
// eleven, and the admission-policies pair drops to ZERO: both sides produce the
// same 19 resources.

// Config drives the gate.
type Config struct {
	RepoRoot string
	// Registry holds the recorded divergences. Nil means LoadRegistry(RepoRoot);
	// a test injects one so a fixture tree does not have to carry the real
	// registry and decision record.
	Registry *Registry
	// Renderer turns a chart into the manifests it installs. Nil means the real
	// `helm template`; a test injects a double so the gate never needs the
	// toolchain it audits in order to run its own suite.
	Renderer Renderer
}

// Drift describes one chart-pair drift.
type Drift struct {
	Pair        string
	Intentional bool
	// Decision is the docs/decisions.md row the recorded divergences cite.
	Decision        string
	OnlyInPublished []string
	OnlyInBootstrap []string
	// Unapproved lists the drifted files no per-file approval covers. These are
	// what fail the gate — the named files, not the pair.
	Unapproved []string
}

// Outcome captures the result.
type Outcome struct {
	Pass   bool
	Drifts []Drift
	// RBACDrifts carries the per-cell RBAC comparison of each declared pair.
	// Separate from Drifts because the two comparisons answer different
	// questions — "the same TYPES of resource?" and "the same GRANTS?" — and a
	// reader must be able to tell which one a finding came from.
	RBACDrifts []RBACDrift
	// RenderErrors carries a failed `helm template`, verbatim.
	//
	// A render that failed and a chart that produces nothing are different facts:
	// treating the first as an empty set would report total drift as though it
	// had been measured. So it fails the gate and says why.
	RenderErrors []string
}

// Check audits all loopzero-* published charts + conceptual pairs for drift.
func Check(cfg Config) (*Outcome, error) {
	outcome := &Outcome{Pass: true}
	r := cfg.Renderer
	if r == nil {
		r = productionRenderer
	}

	reg := cfg.Registry
	if reg == nil {
		loaded, err := LoadRegistry(cfg.RepoRoot)
		if err != nil {
			return nil, err
		}
		reg = &loaded
	}

	pairs, err := discoverPairs(cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	for _, p := range pairs {
		auditPair(r, *reg, cfg.RepoRoot, p.published, p.bootstrap, outcome)
	}

	return outcome, nil
}

// pair is one published↔bootstrap chart pair this gate audits.
type pair struct{ published, bootstrap string }

// discoverPairs returns the declared pairs, in registry (label) order.
//
// Split out of Check so WHICH pairs this gate audits can be asserted without
// rendering anything.
func discoverPairs(repoRoot string) ([]pair, error) {
	declared, err := chartpairs.LoadRegistry(repoRoot)
	if err != nil {
		return nil, err
	}
	pairs := make([]pair, 0, len(declared))
	for _, d := range declared {
		pairs = append(pairs, pair{published: d.Published, bootstrap: d.Bootstrap})
	}
	return pairs, nil
}

// auditPair compares template sets for one published↔bootstrap pair and, if
// they drift, records a Drift (intentional if the registry records every
// drifted kind; else fails).
func auditPair(r Renderer, reg Registry, repoRoot, publishedName, bootstrapName string, outcome *Outcome) {
	pairKey := publishedName + ":" + bootstrapName
	publishedChart := filepath.Join(repoRoot, "charts", publishedName)
	bootstrapChart := filepath.Join(repoRoot, "infra/helm", bootstrapName)

	if !dirExists(publishedChart) && !dirExists(bootstrapChart) {
		return
	}

	// Compare what the charts INSTALL, not the names of the files that declare
	// it. A `service.yaml` called `svc.yaml` on the other side was reported as a
	// divergence although nothing diverged — measured, 13 of the 21 entries of
	// the old allowlist described exactly that.
	pubKinds, err := renderedKinds(r, publishedChart)
	if err != nil {
		outcome.RenderErrors = append(outcome.RenderErrors, err.Error())
		outcome.Pass = false
		return
	}
	bootKinds, err := renderedKinds(r, bootstrapChart)
	if err != nil {
		outcome.RenderErrors = append(outcome.RenderErrors, err.Error())
		outcome.Pass = false
		return
	}
	onlyPub, onlyBoot := diffKinds(pubKinds, bootKinds)

	// B-001: what the two sides GRANT is a second question, asked of every pair
	// whether or not their resource KINDS agree. A pair can install the same
	// kinds and grant differently -- which is exactly the loopzero-api pair -- so
	// this must not sit behind the early return below.
	auditPairRBAC(r, reg, pairKey, publishedChart, bootstrapChart, outcome)

	if len(onlyPub) == 0 && len(onlyBoot) == 0 {
		return // both sides install the same kinds of resource
	}

	d := Drift{
		Pair:            pairKey,
		OnlyInPublished: onlyPub,
		OnlyInBootstrap: onlyBoot,
	}

	// Every divergent kind carries its own entry. A kind without one fails the
	// pair — the pair being "known to diverge" is no licence for the next
	// resource to diverge in silence.
	for _, k := range append(append([]string{}, onlyPub...), onlyBoot...) {
		if a, ok := reg.kind(pairKey, k); ok {
			d.Decision = a.Decision
			continue
		}
		d.Unapproved = append(d.Unapproved, k)
	}
	d.Intentional = len(d.Unapproved) == 0
	if !d.Intentional {
		outcome.Pass = false
	}
	outcome.Drifts = append(outcome.Drifts, d)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
