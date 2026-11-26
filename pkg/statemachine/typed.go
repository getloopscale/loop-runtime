// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// Typed is the typed surface of a Machine for one lifecycle: S is the
// lifecycle's state type and E its event type, both string-based with the
// same wire values as the untyped State and Event constants, so DB rows, CR
// status fields and SSE payloads round-trip without translation (ADR
// `2026-05-15-T6-1-typed-state-machines.md`).
//
// Each lifecycle names its instantiation with an alias (BuildMachine =
// Typed[BuildPhase, BuildPhaseEvent], and so on) and a constructor that wraps
// its untyped machine, so the transition table and the terminal set stay
// single-sourced in the untyped NewXMachine.
type Typed[S ~string, E ~string] struct {
	inner *Machine
}

// newTyped wraps an untyped machine in its typed surface.
func newTyped[S ~string, E ~string](inner *Machine) *Typed[S, E] {
	return &Typed[S, E]{inner: inner}
}

// Transition runs a transition. Panics on invalid input, matching
// Machine.Transition: an invalid transition in code that controls the state
// is a programming error.
func (m *Typed[S, E]) Transition(current S, event E) S {
	return S(m.inner.Transition(State(current), Event(event)))
}

// TryTransition is the non-panicking variant for states read from outside
// the program (DB rows, CR status). The error wraps ErrInvalidTransition.
func (m *Typed[S, E]) TryTransition(current S, event E) (S, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return S(next), nil
}

// SafeTransition is TryTransition under the name the status stores use. It
// is kept as a separate entry point because the MA-7 gate
// (cmd/loopruntime-ops ma7statuslocality) restricts SafeTransition calls to
// api/internal/db/store, where status writes are serialized.
func (m *Typed[S, E]) SafeTransition(current S, event E) (S, error) {
	return m.TryTransition(current, event)
}

// IsTerminal reports whether the state is terminal.
func (m *Typed[S, E]) IsTerminal(s S) bool {
	return m.inner.IsTerminal(State(s))
}

// Name returns the machine name (e.g. "Build").
func (m *Typed[S, E]) Name() string {
	return m.inner.Name()
}

// IsKnownState mirrors Machine.IsKnownState.
func (m *Typed[S, E]) IsKnownState(s S) bool {
	return m.inner.IsKnownState(State(s))
}

// IsKnownEvent mirrors Machine.IsKnownEvent.
func (m *Typed[S, E]) IsKnownEvent(e E) bool {
	return m.inner.IsKnownEvent(Event(e))
}

// MustParseState wraps a raw string into S after validating membership.
// Panics on unknown values.
func (m *Typed[S, E]) MustParseState(s string) S {
	return S(m.inner.MustParseState(s))
}

// MustParseEvent wraps a raw string into E after validating membership.
func (m *Typed[S, E]) MustParseEvent(e string) E {
	return E(m.inner.MustParseEvent(e))
}

// ValidEvents returns the events valid from the given state; nil for a
// terminal or unknown state.
func (m *Typed[S, E]) ValidEvents(s S) []E {
	untyped := m.inner.ValidEvents(State(s))
	if untyped == nil {
		return nil
	}
	out := make([]E, len(untyped))
	for i, e := range untyped {
		out[i] = E(e)
	}
	return out
}

// Timeouts returns the machine's per-state timeouts.
func (m *Typed[S, E]) Timeouts() []TimeoutConfig {
	return m.inner.Timeouts()
}
