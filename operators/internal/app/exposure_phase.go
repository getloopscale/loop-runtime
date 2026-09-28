// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"fmt"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// routeObservation is what one ExposedRoute reconcile saw.
type routeObservation struct {
	certReady       bool // the TLS certificate exists (or TLS is disabled)
	probeOK         bool // the reachability probe answered 2xx/3xx
	probesExhausted bool // consecutive probe failures reached the limit
}

// maxRouteSteps bounds the event walk of one reconcile. The longest declared
// path is Failed → Pending → CertIssued → Reachable.
const maxRouteSteps = 4

// knownRoutePhases are the states the ExposedRoute machine declares.
var knownRoutePhases = map[statemachine.ExposedRouteState]bool{
	statemachine.XRStateEmpty: true, statemachine.XRStatePending: true, statemachine.XRStateCertIssued: true,
	statemachine.XRStateReachable: true, statemachine.XRStateFailed: true,
}

// nextRouteEvent is the event an observation implies from phase, or "" when
// the observation is already explained by it. The table the ExposedRoute
// machine declares decides where each event leads.
func nextRouteEvent(phase statemachine.ExposedRouteState, obs routeObservation) statemachine.ExposedRouteEvent {
	switch phase {
	case statemachine.XRStateEmpty:
		return statemachine.XREventCreate
	case statemachine.XRStatePending:
		if obs.certReady {
			return statemachine.XREventCertReady
		}
	case statemachine.XRStateCertIssued:
		if obs.probeOK {
			return statemachine.XREventProbeOK
		}
		if obs.probesExhausted {
			return statemachine.XREventProbeFail
		}
	case statemachine.XRStateReachable:
		if obs.probesExhausted {
			return statemachine.XREventProbeFail
		}
	case statemachine.XRStateFailed:
		if !obs.certReady || obs.probeOK {
			return statemachine.XREventRetry
		}
	}
	return ""
}

// advanceRoutePhase walks the ExposedRoute machine from phase, one declared
// transition per observed event, until the observation is explained. A phase
// the machine does not know is an error, never force-set.
func advanceRoutePhase(phase loopzerov1.ExposedRoutePhase, obs routeObservation) (loopzerov1.ExposedRoutePhase, error) {
	state := statemachine.ExposedRouteState(phase)
	if !knownRoutePhases[state] {
		return "", fmt.Errorf("exposed route phase %q is not an ExposedRoute state", phase)
	}
	for step := 0; step < maxRouteSteps; step++ {
		event := nextRouteEvent(state, obs)
		if event == "" {
			return loopzerov1.ExposedRoutePhase(state), nil
		}
		next, err := exposedRouteSM.TryTransition(state, event)
		if err != nil {
			return "", fmt.Errorf("exposed route phase %q on %q: %w", state, event, err)
		}
		state = next
	}
	if nextRouteEvent(state, obs) != "" {
		return "", fmt.Errorf("exposed route phase did not settle within %d transitions (at %q)", maxRouteSteps, state)
	}
	return loopzerov1.ExposedRoutePhase(state), nil
}
