// Package statemachine implements strict state machines for all LoopZero stateful
// entities as defined in the system design spec section 5.
//
// Invalid transitions are programming errors and cause a panic with a
// descriptive message. This is intentional: an invalid transition means the
// caller has a logic bug that must be fixed, not a runtime condition to handle.
package statemachine

import (
	"fmt"
	"time"
)

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
