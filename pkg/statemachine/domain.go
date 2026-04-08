// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Domain states — MEET-2026-04-05-033 §4.
// 8th state machine in the project (CRIT-02 fix: replaces ad-hoc map).
const (
	DomainPendingVerification State = "pending_verification"
	DomainVerified            State = "verified"
	DomainProvisioning        State = "provisioning"
	DomainActive              State = "active"
	DomainDegraded            State = "degraded" // CNAME lost, grace period (14 days before removal)
	DomainFailed              State = "failed"
	DomainRemoving            State = "removing"
)

// Domain events — triggers for state transitions.
const (
	DomainVerify           Event = "DomainVerify"           // TXT record confirmed
	DomainProvision        Event = "DomainProvision"        // Provisioner claims domain
	DomainActivate         Event = "DomainActivate"         // Gitops push succeeded
	DomainProvisionFail    Event = "DomainProvisionFail"    // Build/gitops/render error
	DomainDegrade          Event = "DomainDegrade"          // CNAME check failed → grace period
	DomainCNAMELost        Event = "DomainCNAMELost"        // Grace period expired (14d) → failed
	DomainCNAMERecovered   Event = "DomainCNAMERecovered"   // CNAME recovered during grace period
	DomainRetry            Event = "DomainRetry"            // Retry after failure
	DomainRemove           Event = "DomainRemove"           // User requests removal
	DomainProvisionTimeout Event = "DomainProvisionTimeout" // Stuck > 1 hour
)

// IsDomainTerminal returns true for terminal domain states.
// "removing" is terminal — row is deleted after gitops cleanup.
func IsDomainTerminal(state State) bool {
	return state == DomainRemoving
}

// NewDomainMachine returns the domain state machine per MEET-033 §4.
//
// States:
//   pending_verification → verified → provisioning → active
//                                         ↓              ↓
//                                       failed      degraded (CNAME lost, 14d grace)
//                                         ↑              ↓
//                                         └──────── failed (grace expired)
//   (any non-terminal) → removing
//   failed → verified (retry via loopzero domain verify)
//   degraded → active (CNAME recovered during grace)
//
// Terminal: removing (row deleted after gitops cleanup)
func NewDomainMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Domain",
		Transitions: []transition{
			// Verification
			{From: DomainPendingVerification, Event: DomainVerify, To: DomainVerified},
			{From: DomainPendingVerification, Event: DomainRemove, To: DomainRemoving},

			// Provisioning
			{From: DomainVerified, Event: DomainProvision, To: DomainProvisioning},
			{From: DomainVerified, Event: DomainRemove, To: DomainRemoving},

			// Activation
			{From: DomainProvisioning, Event: DomainActivate, To: DomainActive},
			{From: DomainProvisioning, Event: DomainProvisionFail, To: DomainFailed},
			{From: DomainProvisioning, Event: DomainProvisionTimeout, To: DomainFailed},
			{From: DomainProvisioning, Event: DomainRemove, To: DomainRemoving},

			// Active domain events
			{From: DomainActive, Event: DomainDegrade, To: DomainDegraded},
			{From: DomainActive, Event: DomainRemove, To: DomainRemoving},

			// Degraded domain (CNAME lost, grace period active)
			{From: DomainDegraded, Event: DomainCNAMERecovered, To: DomainActive},
			{From: DomainDegraded, Event: DomainCNAMELost, To: DomainFailed},
			{From: DomainDegraded, Event: DomainRemove, To: DomainRemoving},

			// Failed domain recovery
			{From: DomainFailed, Event: DomainRetry, To: DomainVerified},
			{From: DomainFailed, Event: DomainRemove, To: DomainRemoving},
		},
		Terminal: []State{DomainRemoving},
	})
}
