// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import "fmt"

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
