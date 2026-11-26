// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"strings"
	"testing"
)

func TestVisualize_ContainsHeaderAndAllStates(t *testing.T) {
	m := NewBuildMachine()
	dot := m.Visualize()

	if !strings.HasPrefix(dot, "digraph") {
		t.Errorf("expected DOT header, got: %s", dot)
	}
	if !strings.Contains(dot, "rankdir=LR") {
		t.Error("expected LR layout direction")
	}

	// Every state declared in transitions or terminal map must appear.
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
	for s := range stateSet {
		if !strings.Contains(dot, "\""+string(s)+"\"") {
			t.Errorf("DOT missing state %q", s)
		}
	}
}

func TestVisualize_TerminalStatesUseDoubleCircle(t *testing.T) {
	m := NewBuildMachine()
	dot := m.Visualize()
	for s := range m.terminal {
		// Each terminal should be referenced with shape=doublecircle.
		needle := "\"" + string(s) + "\" [shape=doublecircle]"
		if !strings.Contains(dot, needle) {
			t.Errorf("terminal state %q must be rendered with doublecircle: %s", s, needle)
		}
	}
}

func TestVisualize_IsDeterministic(t *testing.T) {
	m := NewBuildMachine()
	first := m.Visualize()
	for i := 0; i < 5; i++ {
		again := m.Visualize()
		if first != again {
			t.Fatalf("Visualize must be deterministic; iter %d differs", i)
		}
	}
}

func TestVisualize_AllMachinesProduceValidDot(t *testing.T) {
	for name, m := range allMachines() {
		dot := m.Visualize()
		if !strings.HasPrefix(dot, "digraph") {
			t.Errorf("machine %s: invalid DOT prefix", name)
		}
		if !strings.HasSuffix(strings.TrimSpace(dot), "}") {
			t.Errorf("machine %s: missing closing brace", name)
		}
	}
}

func TestDotIdent_HandlesEdgeCases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Build", "Build"},
		{"State Machine", "State_Machine"},
		{"123start", "M_123start"},
		{"", "Unnamed"},
	}
	for _, c := range cases {
		if got := dotIdent(c.in); got != c.want {
			t.Errorf("dotIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
