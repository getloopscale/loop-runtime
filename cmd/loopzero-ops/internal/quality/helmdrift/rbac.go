// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"fmt"
	"sort"
)

// Cell is one (apiGroup, resource, verb) RBAC grant.
//
// # Why the cell, and not the rule
//
// A ClusterRole `PolicyRule` is a cross product. Two rule lists can be textually
// different and grant identically (a rule split in two), or textually almost
// identical and differ by exactly one verb — which is `(core)/secrets/delete`,
// the cell B-001 was registered about. Comparing rendered rule text produces both
// false positives and false negatives; comparing expanded cells produces neither.
type Cell struct {
	APIGroup string
	Resource string
	Verb     string
}

// String renders the cell the way the report and the registry both spell it.
//
// The core API group is the empty string in RBAC. Printing it as an empty field
// gives `/secrets/delete`, which a reader can mistake for a truncation bug, so it
// renders as `(core)` — and the registry keys use the same spelling, so comparing
// a gate failure to a registry entry is a literal string match.
func (c Cell) String() string {
	group := c.APIGroup
	if group == "" {
		group = "(core)"
	}
	return fmt.Sprintf("%s/%s/%s", group, c.Resource, c.Verb)
}

// RBACDrift is one declared pair's grant divergence.
//
// It carries CELLS, never only a count: a bare boolean or a bare number tells a
// reader that something diverged and not whether it is progress or regression
// (B-001 NFR-002).
type RBACDrift struct {
	Pair string
	// OnlyInPublished / OnlyInBootstrap are the divergent cells, unioned over
	// the values matrix and sorted for stable output.
	OnlyInPublished []Cell
	OnlyInBootstrap []Cell
	// Undeclared are the divergent cells no registry entry covers. These are
	// what fail the gate — the named cells, not the pair.
	Undeclared []Cell
	// Undecided are declared cells whose entry records a LIVE, unreviewed
	// asymmetry rather than an approval. They do not fail the gate, and they are
	// listed by name so a green run can never be read as "reviewed".
	Undecided []Cell
	// Declared counts the divergent cells a registry entry covers, so a green
	// verdict never reads as "0 divergent".
	Declared int
}

// ReportLines renders the finding. Both numbers are computed from the cells, and
// every undeclared and undecided cell is named — printing a count alone is the
// shape NFR-002 forbids.
func (d RBACDrift) ReportLines() []string {
	lines := []string{
		fmt.Sprintf("RBAC cells: %d declared, %d undeclared", d.Declared, len(d.Undeclared)),
	}
	for _, c := range d.Undeclared {
		lines = append(lines, "UNDECLARED "+c.String())
	}
	for _, c := range d.Undecided {
		lines = append(lines, "UNDECIDED "+c.String()+" — recorded, not approved")
	}
	return lines
}

// expandRules turns the ClusterRole documents of one render into the SET of
// cells they grant.
//
// Pure: no I/O, so the comparison the gate rests on is unit-testable without the
// toolchain it audits.
//
// # Wildcards are LITERAL
//
// A rule granting `loopzero.io` `*`/`*` becomes the single cell `loopzero.io/*/*`. It is
// not expanded to cover the specific cells the other side names. Expanding would
// require the gate to own a model of RBAC resolution — what `*` covers depends on
// the API server's resource list, which the gate cannot see — and an invented
// model inside a gate is the defect this family of gates exists to catch. The
// cost is declared: a side granting `*` and a side granting the enumeration are
// reported as diverging, which is true of the text and arguably not of the
// effect.
//
// An absent `apiGroups` is the core group, which is RBAC's own default; a rule
// with no verbs or no resources grants nothing and contributes no cell rather
// than panicking.
func expandRules(docs []map[string]any) map[Cell]bool {
	cells := map[Cell]bool{}
	for _, doc := range docs {
		if kind, _ := doc["kind"].(string); kind != "ClusterRole" {
			continue
		}
		rules, _ := doc["rules"].([]any)
		for _, raw := range rules {
			rule, _ := raw.(map[string]any)
			if rule == nil {
				continue
			}
			groups := stringsOf(rule["apiGroups"])
			if len(groups) == 0 {
				groups = []string{""}
			}
			resources := stringsOf(rule["resources"])
			verbs := stringsOf(rule["verbs"])
			for _, g := range groups {
				for _, res := range resources {
					for _, v := range verbs {
						cells[Cell{APIGroup: g, Resource: res, Verb: v}] = true
					}
				}
			}
		}
	}
	return cells
}

// stringsOf reads a YAML list of scalars, skipping anything that is not a string
// so a malformed rule degrades to granting less rather than crashing the gate.
func stringsOf(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// renderedCells renders a chart under `sets` and expands its ClusterRoles.
func renderedCells(r Renderer, chartDir string, sets []string) (map[Cell]bool, error) {
	docs, err := renderDocs(r, chartDir, sets)
	if err != nil {
		return nil, err
	}
	return expandRules(docs), nil
}

// diffCells returns what only one side grants, each sorted so two reports diff
// cleanly.
func diffCells(published, bootstrap map[Cell]bool) (onlyPublished, onlyBootstrap []Cell) {
	for c := range published {
		if !bootstrap[c] {
			onlyPublished = append(onlyPublished, c)
		}
	}
	for c := range bootstrap {
		if !published[c] {
			onlyBootstrap = append(onlyBootstrap, c)
		}
	}
	sortCells(onlyPublished)
	sortCells(onlyBootstrap)
	return onlyPublished, onlyBootstrap
}

func sortCells(cells []Cell) {
	sort.Slice(cells, func(i, j int) bool { return cells[i].String() < cells[j].String() })
}

// auditPairRBAC compares what the two sides of one pair GRANT, and records a
// finding when they differ.
//
// The published side is rendered once per entry of rbacValuesMatrix and so is the
// bootstrap side, and the DIVERGENCES are unioned — not the cell sets. A cell the
// published chart grants only under one flag state still diverges under the
// other, and unioning the cell sets instead would report that as agreement.
//
// Silence is the contract for a converged pair, and for a pair where neither side
// renders a ClusterRole: two of the three real pairs are in the second state, so
// a comparison that spoke up regardless would be reporting on nothing.
func auditPairRBAC(r Renderer, reg Registry, pairKey, publishedChart, bootstrapChart string, outcome *Outcome) {
	onlyPub := map[Cell]bool{}
	onlyBoot := map[Cell]bool{}
	measured := false

	for _, sets := range matrixFor(pairKey) {
		pub, err := renderedCells(r, publishedChart, sets)
		if err != nil {
			outcome.RenderErrors = append(outcome.RenderErrors, err.Error())
			outcome.Pass = false
			return
		}
		boot, err := renderedCells(r, bootstrapChart, sets)
		if err != nil {
			outcome.RenderErrors = append(outcome.RenderErrors, err.Error())
			outcome.Pass = false
			return
		}
		measured = true
		p, b := diffCells(pub, boot)
		for _, c := range p {
			onlyPub[c] = true
		}
		for _, c := range b {
			onlyBoot[c] = true
		}
	}

	if !measured || (len(onlyPub) == 0 && len(onlyBoot) == 0) {
		return
	}

	d := RBACDrift{Pair: pairKey}
	for c := range onlyPub {
		d.OnlyInPublished = append(d.OnlyInPublished, c)
	}
	for c := range onlyBoot {
		d.OnlyInBootstrap = append(d.OnlyInBootstrap, c)
	}
	sortCells(d.OnlyInPublished)
	sortCells(d.OnlyInBootstrap)

	// Every divergent cell carries its own declaration. A pair being "known to
	// diverge" is not a licence for the next cell to diverge in silence — that
	// pair-wide blessing is how a ValidatingAdmissionPolicy left the published
	// chart unnoticed, and the kind registry was re-keyed for the same reason.
	for _, c := range append(append([]Cell{}, d.OnlyInPublished...), d.OnlyInBootstrap...) {
		a, ok := reg.cell(pairKey, c)
		if !ok {
			d.Undeclared = append(d.Undeclared, c)
			continue
		}
		d.Declared++
		if a.Status == statusUndecided {
			d.Undecided = append(d.Undecided, c)
		}
	}
	sortCells(d.Undeclared)
	sortCells(d.Undecided)

	if len(d.Undeclared) > 0 {
		outcome.Pass = false
	}
	outcome.RBACDrifts = append(outcome.RBACDrifts, d)
}
