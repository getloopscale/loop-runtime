// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// PlatformBootstrap states as defined in the CRD (runtime.getloopscale.com/v1 PlatformBootstrap).
// PlatformBootstrapEmpty represents the initial zero-value before first reconcile.
const (
	PlatformBootstrapEmpty            State = ""
	PlatformBootstrapPending          State = "Pending"
	PlatformBootstrapConfiguringGitea State = "ConfiguringGitea"
	PlatformBootstrapConfiguringArgo  State = "ConfiguringArgoCD"
	PlatformBootstrapWaitingForAPI    State = "WaitingForAPI"
	PlatformBootstrapComplete         State = "BootstrapComplete"
	PlatformBootstrapFailed           State = "Failed"
)

// PlatformBootstrap events that trigger state transitions.
const (
	PlatformBootstrapStart        Event = "Start"
	PlatformBootstrapGiteaReady   Event = "GiteaReady"
	PlatformBootstrapArgoCDReady  Event = "ArgoCDReady"
	PlatformBootstrapAPISeeded    Event = "APISeeded"
	PlatformBootstrapRetryExhaust Event = "RetryExhausted"
)

// NewPlatformBootstrapMachine returns the PlatformBootstrap lifecycle state machine.
//
// Transitions:
//
//	"" (empty)           --Start-->         Pending
//	Pending              --Start-->         ConfiguringGitea
//	ConfiguringGitea     --GiteaReady-->    ConfiguringArgoCD
//	ConfiguringGitea     --RetryExhausted-->Failed
//	ConfiguringArgoCD    --ArgoCDReady-->   WaitingForAPI
//	ConfiguringArgoCD    --RetryExhausted-->Failed
//	WaitingForAPI        --APISeeded-->     BootstrapComplete
//	WaitingForAPI        --RetryExhausted-->Failed
//
// Terminal: BootstrapComplete, Failed
func NewPlatformBootstrapMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "PlatformBootstrap",
		Transitions: []transition{
			{From: PlatformBootstrapEmpty, Event: PlatformBootstrapStart, To: PlatformBootstrapPending},
			{From: PlatformBootstrapPending, Event: PlatformBootstrapStart, To: PlatformBootstrapConfiguringGitea},
			{From: PlatformBootstrapConfiguringGitea, Event: PlatformBootstrapGiteaReady, To: PlatformBootstrapConfiguringArgo},
			{From: PlatformBootstrapConfiguringGitea, Event: PlatformBootstrapRetryExhaust, To: PlatformBootstrapFailed},
			{From: PlatformBootstrapConfiguringArgo, Event: PlatformBootstrapArgoCDReady, To: PlatformBootstrapWaitingForAPI},
			{From: PlatformBootstrapConfiguringArgo, Event: PlatformBootstrapRetryExhaust, To: PlatformBootstrapFailed},
			{From: PlatformBootstrapWaitingForAPI, Event: PlatformBootstrapAPISeeded, To: PlatformBootstrapComplete},
			{From: PlatformBootstrapWaitingForAPI, Event: PlatformBootstrapRetryExhaust, To: PlatformBootstrapFailed},
		},
		Terminal: []State{PlatformBootstrapComplete, PlatformBootstrapFailed},
	})
}
