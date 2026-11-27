// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
		// MEET-052: PendingDeletion state (grace period before hard delete)
		{"active to pending deletion", TenantActive, TenantRequestDeletion, TenantPendingDeletion},
		{"suspended to pending deletion", TenantSuspended, TenantRequestDeletion, TenantPendingDeletion},
		{"pending deletion to deleting on grace expiry", TenantPendingDeletion, TenantGraceExpired, TenantDeleting},
		{"pending deletion to active on restore", TenantPendingDeletion, TenantRestore, TenantActive},
		{"pending deletion to deleting on force", TenantPendingDeletion, TenantDelete, TenantDeleting},
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

func TestTenantMachine_PendingDeletionIsNotTerminal(t *testing.T) {
	m := NewTenantMachine()
	if m.IsTerminal(TenantPendingDeletion) {
		t.Fatal("PendingDeletion should NOT be terminal — it can transition to Active (restore) or Deleting")
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
		{"active cannot restore (not pending deletion)", TenantActive, TenantRestore},
		{"provisioning cannot request deletion directly to deleting", TenantProvisioning, TenantRequestDeletion},
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
