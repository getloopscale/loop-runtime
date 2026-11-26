// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Domain states — MEET-2026-04-05-033 §4, updated for CF Zone Management v2.
// 8th state machine in the project (CRIT-02 fix: replaces ad-hoc map).
const (
	// v1 states (TXT verification flow — backward compat for legacy domains)
	DomainPendingVerification State = "pending_verification"
	DomainVerified            State = "verified"

	// v2 states (CF Zone flow — nameserver delegation)
	DomainPendingNS State = "pending_ns" // Zone created, waiting for NS propagation
	DomainNSActive  State = "ns_active"  // NS propagated, waiting for deploy to provision

	// Shared states (both v1 and v2)
	DomainProvisioning State = "provisioning"
	DomainActive       State = "active"
	DomainDegraded     State = "degraded" // CNAME lost, grace period (14 days before removal)
	DomainFailed       State = "failed"
	DomainRemoving     State = "removing"
)

// Domain events — triggers for state transitions.
const (
	// v1 events
	DomainVerify         Event = "DomainVerify"         // TXT record confirmed
	DomainDegrade        Event = "DomainDegrade"        // CNAME check failed → grace period
	DomainCNAMELost      Event = "DomainCNAMELost"      // Grace period expired (14d) → failed
	DomainCNAMERecovered Event = "DomainCNAMERecovered" // CNAME recovered during grace period

	// v2 events
	DomainNSActivated Event = "DomainNSActivated" // CF zone nameservers propagated
	DomainNSTimeout   Event = "DomainNSTimeout"   // NS not propagated within 48h

	// Shared events (both v1 and v2)
	DomainProvision        Event = "DomainProvision"        // Provisioner claims domain
	DomainActivate         Event = "DomainActivate"         // Gitops push succeeded
	DomainProvisionFail    Event = "DomainProvisionFail"    // Build/gitops/render error
	DomainRetry            Event = "DomainRetry"            // Retry after failure
	DomainRemove           Event = "DomainRemove"           // User requests removal
	DomainProvisionTimeout Event = "DomainProvisionTimeout" // Stuck > 1 hour
	DomainAppRemoved       Event = "DomainAppRemoved"       // The running release no longer has the domain's app
)

// IsDomainTerminal returns true for terminal domain states.
// "removing" is terminal — row is deleted after gitops cleanup.
func IsDomainTerminal(state State) bool {
	return domainLifecycle.IsTerminal(state)
}

// domainLifecycle answers the package-level Domain helpers from the one
// transition table. A Machine is immutable, so sharing it is safe.
var domainLifecycle = NewDomainMachine()

// NewDomainMachine returns the domain state machine.
//
// v1 flow (TXT verification — legacy):
//
//	pending_verification → verified → provisioning → active
//
// v2 flow (CF Zone Management):
//
//	pending_ns → ns_active → provisioning → active
//
// Shared:
//
//	provisioning → failed (on error/timeout)
//	active → degraded → failed (CNAME grace period, v1 only)
//	failed → verified/ns_active (retry)
//	(any non-terminal) → removing
//
// Terminal: removing (row deleted after gitops cleanup)
func NewDomainMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Domain",
		Transitions: []transition{
			// --- v1: TXT verification flow (backward compat) ---
			{From: DomainPendingVerification, Event: DomainVerify, To: DomainVerified},
			{From: DomainPendingVerification, Event: DomainRemove, To: DomainRemoving},
			{From: DomainVerified, Event: DomainProvision, To: DomainProvisioning},
			{From: DomainVerified, Event: DomainRemove, To: DomainRemoving},

			// --- v2: CF Zone flow (nameserver delegation) ---
			{From: DomainPendingNS, Event: DomainNSActivated, To: DomainNSActive},
			{From: DomainPendingNS, Event: DomainNSTimeout, To: DomainFailed},
			{From: DomainPendingNS, Event: DomainRemove, To: DomainRemoving},
			{From: DomainNSActive, Event: DomainProvision, To: DomainProvisioning},
			{From: DomainNSActive, Event: DomainRemove, To: DomainRemoving},

			// --- Shared: Provisioning ---
			{From: DomainProvisioning, Event: DomainActivate, To: DomainActive},
			{From: DomainProvisioning, Event: DomainProvisionFail, To: DomainFailed},
			{From: DomainProvisioning, Event: DomainProvisionTimeout, To: DomainFailed},
			{From: DomainProvisioning, Event: DomainRemove, To: DomainRemoving},

			// --- Shared: Active ---
			{From: DomainActive, Event: DomainDegrade, To: DomainDegraded},
			{From: DomainActive, Event: DomainRemove, To: DomainRemoving},

			// --- v1: Degraded (CNAME lost, grace period) ---
			{From: DomainDegraded, Event: DomainCNAMERecovered, To: DomainActive},
			{From: DomainDegraded, Event: DomainCNAMELost, To: DomainFailed},
			{From: DomainActive, Event: DomainAppRemoved, To: DomainFailed},
			{From: DomainDegraded, Event: DomainAppRemoved, To: DomainFailed},
			{From: DomainDegraded, Event: DomainRemove, To: DomainRemoving},

			// --- Shared: Failed recovery ---
			{From: DomainFailed, Event: DomainRetry, To: DomainVerified},       // v1 retry (TXT flow)
			{From: DomainFailed, Event: DomainNSActivated, To: DomainNSActive}, // v2 retry (NS came back)
			{From: DomainFailed, Event: DomainRemove, To: DomainRemoving},

			// --- v2: Degraded from provisioning (CNAME lost during setup) ---
			{From: DomainProvisioning, Event: DomainDegrade, To: DomainDegraded},
		},
		Terminal: []State{DomainRemoving},
	})
}
