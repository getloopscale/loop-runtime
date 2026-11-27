// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// Tenant states as defined in the CRD (runtime.getloopscale.com/v1 Tenant).
// TenantEmpty represents the initial zero-value before first reconcile.
const (
	TenantEmpty           State = ""
	TenantProvisioning    State = "Provisioning"
	TenantActive          State = "Active"
	TenantSuspended       State = "Suspended"
	TenantPendingDeletion State = "PendingDeletion" // MEET-052: grace period before hard delete
	TenantDeleting        State = "Deleting"
)

// Tenant events that trigger state transitions.
const (
	TenantCreate          Event = "Create"
	TenantAllReady        Event = "AllConditionsReady"
	TenantSpecChanged     Event = "SpecChanged"
	TenantSuspend         Event = "Suspend"
	TenantUnsuspend       Event = "Unsuspend"
	TenantRequestDeletion Event = "RequestDeletion" // MEET-052: soft delete with grace period
	TenantGraceExpired    Event = "GraceExpired"    // MEET-052: grace period elapsed
	TenantRestore         Event = "Restore"         // MEET-052: cancel pending deletion
	TenantDelete          Event = "Delete"
)

// NewTenantMachine returns the Tenant lifecycle state machine.
//
// Transitions:
//
//	"" (empty)        --Create--> Provisioning
//	Provisioning      --AllConditionsReady--> Active
//	Provisioning      --Delete--> Deleting
//	Active            --SpecChanged--> Provisioning  (re-reconcile)
//	Active            --Suspend--> Suspended
//	Active            --Delete--> Deleting           (force delete, no grace)
//	Active            --RequestDeletion--> PendingDeletion  (MEET-052: soft delete)
//	Suspended         --Unsuspend--> Active
//	Suspended         --Delete--> Deleting
//	Suspended         --RequestDeletion--> PendingDeletion  (MEET-052: soft delete)
//	PendingDeletion   --GraceExpired--> Deleting     (MEET-052: grace period elapsed)
//	PendingDeletion   --Delete--> Deleting           (MEET-052: force delete during grace)
//	PendingDeletion   --Restore--> Active            (MEET-052: cancel deletion)
//
// Terminal: Deleting (finalizer removal completes object deletion)
func NewTenantMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Tenant",
		Transitions: []transition{
			{From: TenantEmpty, Event: TenantCreate, To: TenantProvisioning},
			{From: TenantEmpty, Event: TenantDelete, To: TenantDeleting},
			{From: TenantProvisioning, Event: TenantAllReady, To: TenantActive},
			{From: TenantProvisioning, Event: TenantDelete, To: TenantDeleting},
			{From: TenantActive, Event: TenantSpecChanged, To: TenantProvisioning},
			{From: TenantActive, Event: TenantSuspend, To: TenantSuspended},
			{From: TenantActive, Event: TenantDelete, To: TenantDeleting},
			{From: TenantActive, Event: TenantRequestDeletion, To: TenantPendingDeletion},
			{From: TenantSuspended, Event: TenantUnsuspend, To: TenantActive},
			{From: TenantSuspended, Event: TenantDelete, To: TenantDeleting},
			{From: TenantSuspended, Event: TenantRequestDeletion, To: TenantPendingDeletion},
			{From: TenantPendingDeletion, Event: TenantGraceExpired, To: TenantDeleting},
			{From: TenantPendingDeletion, Event: TenantDelete, To: TenantDeleting},
			{From: TenantPendingDeletion, Event: TenantRestore, To: TenantActive},
		},
		Terminal: []State{TenantDeleting},
	})
}
