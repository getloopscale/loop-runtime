package statemachine

// Tenant states as defined in the CRD (loopzero.io/v1 Tenant).
// TenantEmpty represents the initial zero-value before first reconcile.
const (
	TenantEmpty        State = ""
	TenantProvisioning State = "Provisioning"
	TenantActive       State = "Active"
	TenantSuspended    State = "Suspended"
	TenantDeleting     State = "Deleting"
)

// Tenant events that trigger state transitions.
const (
	TenantCreate      Event = "Create"
	TenantAllReady    Event = "AllConditionsReady"
	TenantSpecChanged Event = "SpecChanged"
	TenantSuspend     Event = "Suspend"
	TenantUnsuspend   Event = "Unsuspend"
	TenantDelete      Event = "Delete"
)

// NewTenantMachine returns the Tenant lifecycle state machine.
//
// Transitions:
//
//	"" (empty)     --Create--> Provisioning
//	Provisioning   --AllConditionsReady--> Active
//	Provisioning   --Delete--> Deleting
//	Active         --SpecChanged--> Provisioning  (re-reconcile)
//	Active         --Suspend--> Suspended
//	Active         --Delete--> Deleting
//	Suspended      --Unsuspend--> Active
//	Suspended      --Delete--> Deleting
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
			{From: TenantSuspended, Event: TenantUnsuspend, To: TenantActive},
			{From: TenantSuspended, Event: TenantDelete, To: TenantDeleting},
		},
		Terminal: []State{TenantDeleting},
	})
}
