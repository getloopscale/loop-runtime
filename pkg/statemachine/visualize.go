// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:generate go run ./cmd/gen-dots --write --out ../../docs/state-machines

package statemachine

import (
	"fmt"
	"sort"
	"strings"
)

// Visualize returns a Graphviz DOT representation of the state machine.
// Terminal states are rendered with a double circle (kpt convention).
// The output is deterministic (states + transitions sorted lex) so it
// can be diffed in PRs and consumed by `dot -Tsvg` to produce
// publishable SVG.
//
// Per pkg-sota-faang-grade-plan T2.7 + gate PKG-G18.
//
// Example pipeline:
//
//	dot := NewBuildMachine().Visualize()
//	os.WriteFile("docs/state-machines/build.dot", []byte(dot), 0644)
//	exec.Command("dot", "-Tsvg", "docs/state-machines/build.dot",
//	             "-o", "docs/state-machines/build.svg").Run()
//
// cmd/gen-dots writes one DOT file per machine (`go generate`, or
// `task pkg:state-machine-dots:write`); `task pkg:state-machine-dots:check`
// fails when a committed DOT no longer matches Visualize(), which catches
// transition drift in PR review.
func (m *Machine) Visualize() string {
	var b strings.Builder
	fmt.Fprintf(&b, "digraph %s {\n", dotIdent(m.name))
	b.WriteString("\trankdir=LR;\n")
	b.WriteString("\tnode [shape=circle, fontsize=10];\n")
	b.WriteString("\tedge [fontsize=8];\n\n")

	// Collect every state referenced (sources + destinations + terminals).
	stateSet := make(map[State]struct{})
	for from, events := range m.transitions {
		stateSet[from] = struct{}{}
		for _, to := range events {
			stateSet[to] = struct{}{}
		}
	}
	for s := range m.terminal {
		stateSet[s] = struct{}{}
	}

	// Sort states lex for deterministic output.
	states := make([]State, 0, len(stateSet))
	for s := range stateSet {
		states = append(states, s)
	}
	sort.Slice(states, func(i, j int) bool { return states[i] < states[j] })

	// Emit nodes (terminal => doublecircle).
	for _, s := range states {
		shape := "circle"
		if m.terminal[s] {
			shape = "doublecircle"
		}
		fmt.Fprintf(&b, "\t%q [shape=%s];\n", string(s), shape)
	}
	b.WriteString("\n")

	// Emit edges in deterministic order.
	type edge struct {
		from, event, to string
	}
	var edges []edge
	for from, events := range m.transitions {
		for ev, to := range events {
			edges = append(edges, edge{string(from), string(ev), string(to)})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		if edges[i].event != edges[j].event {
			return edges[i].event < edges[j].event
		}
		return edges[i].to < edges[j].to
	})
	for _, e := range edges {
		fmt.Fprintf(&b, "\t%q -> %q [label=%q];\n", e.from, e.to, e.event)
	}

	b.WriteString("}\n")
	return b.String()
}

// dotIdent returns a DOT-safe identifier (alphanumeric + underscore)
// for the digraph header. Non-conforming characters become '_'.
func dotIdent(s string) string {
	if s == "" {
		return "Unnamed"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "M_" + out
	}
	return out
}
