package statemachine

import (
	"testing"
)

func TestTenantMachine_ValidTransitions(t *testing.T) {
	m := NewTenantMachine()

	tests := []struct {
		name  string
		from  State
		event Event
		to    State
	}{
		{"empty to provisioning", TenantEmpty, TenantCreate, TenantProvisioning},
		{"empty to deleting", TenantEmpty, TenantDelete, TenantDeleting},
		{"provisioning to active", TenantProvisioning, TenantAllReady, TenantActive},
		{"provisioning to deleting", TenantProvisioning, TenantDelete, TenantDeleting},
		{"active to provisioning on spec change", TenantActive, TenantSpecChanged, TenantProvisioning},
		{"active to suspended", TenantActive, TenantSuspend, TenantSuspended},
		{"active to deleting", TenantActive, TenantDelete, TenantDeleting},
		{"suspended to active", TenantSuspended, TenantUnsuspend, TenantActive},
		{"suspended to deleting", TenantSuspended, TenantDelete, TenantDeleting},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.Transition(tt.from, tt.event)
			if got != tt.to {
				t.Fatalf("Transition(%q, %q) = %q, want %q", tt.from, tt.event, got, tt.to)
			}
		})
	}
}

func TestTenantMachine_DeletingIsTerminal(t *testing.T) {
	m := NewTenantMachine()
	if !m.IsTerminal(TenantDeleting) {
		t.Fatal("expected Deleting to be terminal")
	}
}

func TestTenantMachine_InvalidTransitionPanics(t *testing.T) {
	m := NewTenantMachine()

	tests := []struct {
		name  string
		from  State
		event Event
	}{
		{"deleting is terminal", TenantDeleting, TenantCreate},
		{"provisioning cannot suspend", TenantProvisioning, TenantSuspend},
		{"empty cannot be active", TenantEmpty, TenantAllReady},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for Transition(%q, %q), got none", tt.from, tt.event)
				}
			}()
			m.Transition(tt.from, tt.event)
		})
	}
}

func TestTenantMachine_Name(t *testing.T) {
	m := NewTenantMachine()
	if m.Name() != "Tenant" {
		t.Fatalf("expected name=Tenant, got %q", m.Name())
	}
}
