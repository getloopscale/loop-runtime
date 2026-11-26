// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
	"testing/quick"
)

// allMachines returns every state machine the package exports.
// Used by property tests to exercise invariants across the entire
// catalogue — adding a new machine without registering here is a
// regression we want to catch.
func allMachines() map[string]*Machine {
	return map[string]*Machine{
		"Build":             NewBuildMachine(),
		"Release":           NewReleaseMachine(),
		"Tenant":            NewTenantMachine(),
		"Domain":            NewDomainMachine(),
		"Environment":       NewEnvironmentMachine(),
		"PlatformBootstrap": NewPlatformBootstrapMachine(),
		"Promotion":         NewPromotionMachine(),
		"Rollback":          NewRollbackMachine(),
		"Application":       NewApplicationMachine(),
		"ExposedRoute":      NewExposedRouteMachine(),
		"Preview":           NewPreviewMachine(),
	}
}

// TestProperty_TerminalStatesRejectAllEvents verifies that every
// terminal state of every machine refuses every random event.
// Per ADR-0035 + gate PKG-G18.
func TestProperty_TerminalStatesRejectAllEvents(t *testing.T) {
	for name, m := range allMachines() {
		// Find terminals declared by the machine.
		var terminals []State
		for s := range m.terminal {
			terminals = append(terminals, s)
		}
		if len(terminals) == 0 {
			continue // some machines have no terminal state
		}

		f := func(rawEvent string) bool {
			ev := Event(rawEvent)
			for _, term := range terminals {
				_, err := m.TryTransition(term, ev)
				if err == nil {
					return false
				}
			}
			return true
		}
		if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
			t.Errorf("machine %s: terminal state rejection property failed: %v", name, err)
		}
	}
}

// TestProperty_TransitionsAreDeterministic verifies that the same
// (state, event) pair always returns the same destination — multiple
// destinations for the same key would be a programming bug.
func TestProperty_TransitionsAreDeterministic(t *testing.T) {
	for name, m := range allMachines() {
		// Build a flat list of declared (from, event) pairs from the
		// transitions map.
		var pairs [][2]string
		for from, events := range m.transitions {
			for ev := range events {
				pairs = append(pairs, [2]string{string(from), string(ev)})
			}
		}

		// Sample each pair 100 times: every call must return the same
		// destination. (Determinism here is structural — but the test
		// guards against any future caching/randomness regression.)
		for _, p := range pairs {
			from := State(p[0])
			ev := Event(p[1])
			first, err := m.TryTransition(from, ev)
			if err != nil {
				t.Errorf("machine %s: declared (state=%s,event=%s) returned error: %v", name, from, ev, err)
				continue
			}
			for i := 0; i < 100; i++ {
				next, err := m.TryTransition(from, ev)
				if err != nil || next != first {
					t.Errorf("machine %s: non-deterministic (state=%s,event=%s) iter %d: first=%s now=%s err=%v",
						name, from, ev, i, first, next, err)
					break
				}
			}
		}
	}
}

// TestProperty_RandomEventsFromValidStateNeverPanic exercises
// TryTransition with random event strings from EVERY known state
// (including non-terminal). The contract is: TryTransition NEVER
// panics — invalid transitions become errors. This complements
// Transition() which panics by design.
func TestProperty_RandomEventsFromValidStateNeverPanic(t *testing.T) {
	for name, m := range allMachines() {
		// Collect declared "from" states (non-terminal start points).
		var fromStates []State
		for s := range m.transitions {
			fromStates = append(fromStates, s)
		}

		f := func(rawEvent string) (ok bool) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("machine %s: TryTransition panicked on random event %q: %v",
						name, rawEvent, r)
					ok = false
				}
			}()
			ev := Event(rawEvent)
			for _, from := range fromStates {
				_, _ = m.TryTransition(from, ev)
			}
			return true
		}
		if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
			t.Errorf("machine %s: TryTransition robustness property failed: %v", name, err)
		}
	}
}

// TestProperty_ValidEventsListMatchesTransitionMap pins the
// invariant that ValidEvents(state) returns exactly the same set of
// keys as the internal transitions map for that state.
func TestProperty_ValidEventsListMatchesTransitionMap(t *testing.T) {
	for name, m := range allMachines() {
		for from, eventsMap := range m.transitions {
			got := m.ValidEvents(from)
			if len(got) != len(eventsMap) {
				t.Errorf("machine %s: ValidEvents(%s) returned %d events, transitions map has %d",
					name, from, len(got), len(eventsMap))
				continue
			}
			gotSet := make(map[Event]struct{}, len(got))
			for _, e := range got {
				gotSet[e] = struct{}{}
			}
			for e := range eventsMap {
				if _, ok := gotSet[e]; !ok {
					t.Errorf("machine %s: ValidEvents(%s) missing event %q",
						name, from, e)
				}
			}
		}
	}
}
