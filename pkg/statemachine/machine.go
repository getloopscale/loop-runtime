// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package statemachine implements strict state machines for all LoopRuntime stateful
// entities as defined in the system design spec section 5.
//
// Invalid transitions are programming errors and cause a panic with a
// descriptive message. This is intentional: an invalid transition means the
// caller has a logic bug that must be fixed, not a runtime condition to handle.
package statemachine

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidTransition is wrapped by every TryTransition / SafeTransition
// error: an unknown source state, an event not valid from the source state,
// or a terminal source state. The wrapping error keeps the machine name,
// state and event.
var ErrInvalidTransition = errors.New("statemachine: invalid transition")

// State represents a state in a state machine.
type State string

// Event represents a trigger that causes a state transition.
type Event string

// TimeoutConfig defines timeout durations for specific states.
type TimeoutConfig struct {
	State   State
	Timeout time.Duration
	Reason  string
}

// transition defines a single valid state transition.
type transition struct {
	From  State
	Event Event
	To    State
}

// Machine is a generic state machine with explicit valid transitions and
// terminal states. It is immutable after creation.
type Machine struct {
	name        string
	transitions map[State]map[Event]State
	terminal    map[State]bool
	timeouts    []TimeoutConfig
}

// MachineConfig holds the configuration for building a Machine.
type MachineConfig struct {
	Name        string
	Transitions []transition
	Terminal    []State
	Timeouts    []TimeoutConfig
}

// NewMachine creates a new state machine from the given configuration.
// It panics if the configuration is invalid (duplicate transitions, terminal
// states with outgoing transitions, etc.).
func NewMachine(cfg MachineConfig) *Machine {
	m := &Machine{
		name:        cfg.Name,
		transitions: make(map[State]map[Event]State),
		terminal:    make(map[State]bool),
		timeouts:    cfg.Timeouts,
	}

	for _, s := range cfg.Terminal {
		m.terminal[s] = true
	}

	for _, t := range cfg.Transitions {
		if m.terminal[t.From] {
			panic(fmt.Sprintf(
				"statemachine %s: terminal state %q cannot have outgoing transition on event %q",
				cfg.Name, t.From, t.Event,
			))
		}
		if m.transitions[t.From] == nil {
			m.transitions[t.From] = make(map[Event]State)
		}
		if existing, ok := m.transitions[t.From][t.Event]; ok {
			panic(fmt.Sprintf(
				"statemachine %s: duplicate transition from %q on event %q (to %q and %q)",
				cfg.Name, t.From, t.Event, existing, t.To,
			))
		}
		m.transitions[t.From][t.Event] = t.To
	}

	return m
}

// Transition returns the new state after applying the given event to the
// current state. It panics if the transition is invalid.
func (m *Machine) Transition(current State, event Event) State {
	if m.terminal[current] {
		panic(fmt.Sprintf(
			"statemachine %s: invalid transition — state %q is terminal, cannot process event %q",
			m.name, current, event,
		))
	}

	events, ok := m.transitions[current]
	if !ok {
		panic(fmt.Sprintf(
			"statemachine %s: invalid transition — no transitions defined from state %q (event: %q)",
			m.name, current, event,
		))
	}

	next, ok := events[event]
	if !ok {
		panic(fmt.Sprintf(
			"statemachine %s: invalid transition — event %q is not valid from state %q",
			m.name, current, event,
		))
	}

	return next
}

// TryTransition attempts a state transition and returns an error instead of
// panicking on invalid transitions. Use this when the current state comes from
// an external source (e.g., CR status) that may have been manually edited.
func (m *Machine) TryTransition(current State, event Event) (State, error) {
	if m.terminal[current] {
		return "", fmt.Errorf("%w: machine %s: state %q is terminal, cannot process event %q",
			ErrInvalidTransition, m.name, current, event)
	}

	events, ok := m.transitions[current]
	if !ok {
		return "", fmt.Errorf("%w: machine %s: no transitions defined from state %q (event: %q)",
			ErrInvalidTransition, m.name, current, event)
	}

	next, ok := events[event]
	if !ok {
		return "", fmt.Errorf("%w: machine %s: event %q is not valid from state %q",
			ErrInvalidTransition, m.name, event, current)
	}

	return next, nil
}

// IsTerminal returns true if the given state is a terminal state.
func (m *Machine) IsTerminal(state State) bool {
	return m.terminal[state]
}

// Timeouts returns the timeout configurations for this machine.
func (m *Machine) Timeouts() []TimeoutConfig {
	return m.timeouts
}

// Name returns the name of this state machine.
func (m *Machine) Name() string {
	return m.name
}

// ValidEvents returns the list of valid events from the given state.
// Returns nil for terminal states.
func (m *Machine) ValidEvents(state State) []Event {
	events, ok := m.transitions[state]
	if !ok {
		return nil
	}
	result := make([]Event, 0, len(events))
	for e := range events {
		result = append(result, e)
	}
	return result
}

// Audit gap M11 #25 (active-execution-plan §6.F.2): State and Event are
// declared as `type X string` aliases. A literal cast like `State("typo")`
// compiles silently — the type system alone cannot reject unknown values.
//
// Migrating to typed enums (e.g., `type State int` + `stringer`) is
// REJECTED for this audit gap because it would break:
//   - DB persistence (schema CHECK constraints compare string values)
//   - JSON marshaling of CR status / API responses
//   - K8s status field conversions across the operator surface
//
// Pragmatic mitigation: validate at trust boundaries (DB reads, JSON
// unmarshaling, manual status edits) via the helpers below. Callers
// that adopt MustParseState / MustParseEvent get fail-fast detection
// of unknown values with a clear panic message, instead of the silent
// drift that a raw `State(stringFromDB)` cast permits — followed by a
// confusing "invalid transition" panic many lines later.
//
// IsKnownState / IsKnownEvent are the non-panicking equivalents for
// callers that want to validate before deciding how to react (e.g.,
// preserve the old state and emit a metric instead of crashing).

// IsKnownState reports whether s is a state used in any transition
// (source, destination) or declared terminal. Useful for validating
// values read from external sources (DB rows, K8s CR status fields,
// JSON request bodies) before treating them as a valid State.
func (m *Machine) IsKnownState(s State) bool {
	if m.terminal[s] {
		return true
	}
	if _, ok := m.transitions[s]; ok {
		return true
	}
	// Also check destination states (states reachable only via transition).
	for _, eventMap := range m.transitions {
		for _, to := range eventMap {
			if to == s {
				return true
			}
		}
	}
	return false
}

// IsKnownEvent reports whether e is an event used in any transition
// for this machine. Mirror of IsKnownState for the event side.
func (m *Machine) IsKnownEvent(e Event) bool {
	for _, eventMap := range m.transitions {
		if _, ok := eventMap[e]; ok {
			return true
		}
	}
	return false
}

// MustParseState wraps a raw string into a State after asserting it is
// known to this machine. Panics with a descriptive message if not —
// matching the rest of the package's "invalid is a programming error"
// posture. Use at trust boundaries instead of raw `State(s)` casts.
func (m *Machine) MustParseState(s string) State {
	state := State(s)
	if !m.IsKnownState(state) {
		panic(fmt.Sprintf(
			"statemachine %s: MustParseState rejected unknown state %q (audit M11 #25 — see pkg/statemachine.IsKnownState for non-panicking variant)",
			m.name, s,
		))
	}
	return state
}

// MustParseEvent wraps a raw string into an Event after asserting it
// is known to this machine. Mirror of MustParseState.
func (m *Machine) MustParseEvent(e string) Event {
	event := Event(e)
	if !m.IsKnownEvent(event) {
		panic(fmt.Sprintf(
			"statemachine %s: MustParseEvent rejected unknown event %q (audit M11 #25 — see pkg/statemachine.IsKnownEvent for non-panicking variant)",
			m.name, e,
		))
	}
	return event
}
