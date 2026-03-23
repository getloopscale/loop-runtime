package health

import (
	"testing"
)

func TestAllPhases_ReturnsAllSixPhases(t *testing.T) {
	phases := AllPhases()

	if len(phases) != 6 {
		t.Fatalf("expected 6 phases, got %d", len(phases))
	}

	expected := map[Phase]bool{
		PhasePending:      false,
		PhaseProvisioning: false,
		PhaseReady:        false,
		PhaseDegraded:     false,
		PhaseDeleting:     false,
		PhaseFailed:       false,
	}

	for _, p := range phases {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected phase: %q", p)
		}
		expected[p] = true
	}

	for p, seen := range expected {
		if !seen {
			t.Errorf("missing phase: %q", p)
		}
	}
}

func TestIsTerminal_OnlyFailedIsTerminal(t *testing.T) {
	tests := []struct {
		phase    Phase
		terminal bool
	}{
		{PhasePending, false},
		{PhaseProvisioning, false},
		{PhaseReady, false},
		{PhaseDegraded, false},
		{PhaseDeleting, false},
		{PhaseFailed, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			got := IsTerminal(tt.phase)
			if got != tt.terminal {
				t.Errorf("IsTerminal(%q) = %v, want %v", tt.phase, got, tt.terminal)
			}
		})
	}
}

func TestValidatePhase_AcceptsAllValidPhases(t *testing.T) {
	for _, p := range AllPhases() {
		t.Run(string(p), func(t *testing.T) {
			if err := ValidatePhase(p); err != nil {
				t.Errorf("ValidatePhase(%q) returned error: %v", p, err)
			}
		})
	}
}

func TestValidatePhase_RejectsInvalidPhase(t *testing.T) {
	invalid := []Phase{"", "Unknown", "ready", "READY", "pending"}

	for _, p := range invalid {
		t.Run(string(p), func(t *testing.T) {
			err := ValidatePhase(p)
			if err == nil {
				t.Errorf("ValidatePhase(%q) should have returned error", p)
			}
		})
	}
}

func TestPhaseStringValues(t *testing.T) {
	// Verify the string values are exactly as documented.
	tests := []struct {
		phase Phase
		want  string
	}{
		{PhasePending, "Pending"},
		{PhaseProvisioning, "Provisioning"},
		{PhaseReady, "Ready"},
		{PhaseDegraded, "Degraded"},
		{PhaseDeleting, "Deleting"},
		{PhaseFailed, "Failed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.phase) != tt.want {
				t.Errorf("phase constant = %q, want %q", tt.phase, tt.want)
			}
		})
	}
}
