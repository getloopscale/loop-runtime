// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

// The package-level terminal helpers answer from the machine's own Terminal
// set, so a state made terminal (or non-terminal) in NewXMachine changes them
// too — the way ReleaseArchived once had to be added in three places.
func TestTerminalHelpersAgreeWithTheirMachines(t *testing.T) {
	cases := []struct {
		machine *Machine
		helper  func(State) bool
		states  []State
	}{
		{NewBuildMachine(), IsBuildTerminal, []State{BuildQueued, BuildRunning, BuildReleasing, BuildSucceeded, BuildFailed, BuildCancelling, BuildCancelled}},
		{NewReleaseMachine(), IsReleaseTerminal, []State{ReleaseReady, ReleasePending, ReleaseActive, ReleaseGitMirrorFailed, ReleaseSyncFailed, ReleaseDriftAlert, ReleaseSuperseded, ReleaseRollingBack, ReleaseRolledBack, ReleaseRollbackFailed, ReleaseCancelled, ReleaseArchived}},
		{NewDomainMachine(), IsDomainTerminal, []State{DomainPendingVerification, DomainVerified, DomainPendingNS, DomainNSActive, DomainProvisioning, DomainActive, DomainDegraded, DomainFailed, DomainRemoving}},
	}
	for _, c := range cases {
		for _, s := range c.states {
			if !c.machine.IsKnownState(s) {
				t.Fatalf("%s: %q is not a state of the machine", c.machine.Name(), s)
			}
			if got, want := c.helper(s), c.machine.IsTerminal(s); got != want {
				t.Errorf("%s: terminal helper(%q) = %v, machine says %v", c.machine.Name(), s, got, want)
			}
		}
	}
}

func TestTypedTerminalHelpersAgreeWithTheUntypedOnes(t *testing.T) {
	for _, p := range []BuildPhase{BPhQueued, BPhRunning, BPhReleasing, BPhSucceeded, BPhFailed, BPhCancelling, BPhCancelled} {
		if IsBuildPhaseTerminal(p) != IsBuildTerminal(State(p)) {
			t.Errorf("build %q: typed and untyped terminal helpers disagree", p)
		}
	}
	for _, p := range []ReleasePhase{RPhReady, RPhActive, RPhSuperseded, RPhCancelled, RPhArchived} {
		if IsReleasePhaseTerminal(p) != IsReleaseTerminal(State(p)) {
			t.Errorf("release %q: typed and untyped terminal helpers disagree", p)
		}
	}
	for _, p := range []DomainPhase{DPhActive, DPhFailed, DPhRemoving} {
		if IsDomainPhaseTerminal(p) != IsDomainTerminal(State(p)) {
			t.Errorf("domain %q: typed and untyped terminal helpers disagree", p)
		}
	}
}
