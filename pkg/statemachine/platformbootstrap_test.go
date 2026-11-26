// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

func TestPlatformBootstrapMachine_HappyPath(t *testing.T) {
	m := NewPlatformBootstrapMachine()

	// "" -> Pending
	s := m.Transition(PlatformBootstrapEmpty, PlatformBootstrapStart)
	if s != PlatformBootstrapPending {
		t.Fatalf("expected Pending, got %s", s)
	}

	// Pending -> ConfiguringGitea
	s = m.Transition(s, PlatformBootstrapStart)
	if s != PlatformBootstrapConfiguringGitea {
		t.Fatalf("expected ConfiguringGitea, got %s", s)
	}

	// ConfiguringGitea -> ConfiguringArgoCD
	s = m.Transition(s, PlatformBootstrapGiteaReady)
	if s != PlatformBootstrapConfiguringArgo {
		t.Fatalf("expected ConfiguringArgoCD, got %s", s)
	}

	// ConfiguringArgoCD -> WaitingForAPI
	s = m.Transition(s, PlatformBootstrapArgoCDReady)
	if s != PlatformBootstrapWaitingForAPI {
		t.Fatalf("expected WaitingForAPI, got %s", s)
	}

	// WaitingForAPI -> BootstrapComplete
	s = m.Transition(s, PlatformBootstrapAPISeeded)
	if s != PlatformBootstrapComplete {
		t.Fatalf("expected BootstrapComplete, got %s", s)
	}

	if !m.IsTerminal(s) {
		t.Fatal("BootstrapComplete should be terminal")
	}
}

func TestPlatformBootstrapMachine_FailedFromEachPhase(t *testing.T) {
	m := NewPlatformBootstrapMachine()

	phases := []State{
		PlatformBootstrapConfiguringGitea,
		PlatformBootstrapConfiguringArgo,
		PlatformBootstrapWaitingForAPI,
	}

	for _, phase := range phases {
		s := m.Transition(phase, PlatformBootstrapRetryExhaust)
		if s != PlatformBootstrapFailed {
			t.Fatalf("from %s: expected Failed, got %s", phase, s)
		}
	}

	if !m.IsTerminal(PlatformBootstrapFailed) {
		t.Fatal("Failed should be terminal")
	}
}

func TestPlatformBootstrapMachine_TerminalPanics(t *testing.T) {
	m := NewPlatformBootstrapMachine()

	for _, terminal := range []State{PlatformBootstrapComplete, PlatformBootstrapFailed} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for transition from terminal state %s", terminal)
				}
			}()
			m.Transition(terminal, PlatformBootstrapStart)
		}()
	}
}
