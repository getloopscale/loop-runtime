// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// applicationSM is the LoopZeroApplication lifecycle machine. Every phase change
// goes through it (Rule 8): the controller derives EVENTS from what it
// observed and lets the machine decide where they lead.
var applicationSM = statemachine.NewApplicationTypedMachine()

// maxPhaseSteps bounds the event walk of one reconcile. The longest declared
// path an observation can drive is Down → Progressing → Serving → Reachable.
const maxPhaseSteps = 4

// knownPhases are the states the Application machine declares.
var knownPhases = map[statemachine.ApplicationPhase]bool{
	statemachine.APhEmpty: true, statemachine.APhProgressing: true, statemachine.APhServing: true,
	statemachine.APhReachable: true, statemachine.APhDegraded: true, statemachine.APhDown: true,
	statemachine.APhCleaned: true,
}

// applicationObservation is what one reconcile saw of the workload and route.
type applicationObservation struct {
	endpointsReady bool // a workload Pod is Ready (or the route answers)
	reachable      bool // the ExposedRoute probe succeeds
	routeFailed    bool // the route reports a hard failure: endpoints are gone
}

func observe(app *loopzerov1.LoopZeroApplication) applicationObservation {
	failed := false
	for _, c := range app.Status.Conditions {
		if c.Type == conditions.TypeRouteReachable && c.Status == metav1.ConditionFalse && c.Reason == conditions.ReasonFailed {
			failed = true
		}
	}
	return applicationObservation{
		endpointsReady: app.Status.EndpointsReady || app.Status.Reachable,
		reachable:      app.Status.Reachable,
		routeFailed:    failed,
	}
}

// nextEvent is the event an observation implies from phase, or "" when the
// observation is already explained by it.
func nextEvent(phase statemachine.ApplicationPhase, obs applicationObservation) statemachine.ApplicationPhaseEvent {
	switch phase {
	case statemachine.APhEmpty:
		return statemachine.APEventCreate
	case statemachine.APhProgressing:
		if obs.routeFailed {
			return statemachine.APEventEndpointsLost
		}
		if obs.endpointsReady {
			return statemachine.APEventEndpointsReady
		}
	case statemachine.APhServing:
		if obs.routeFailed {
			return statemachine.APEventEndpointsLost
		}
		if obs.reachable {
			return statemachine.APEventRouteOK
		}
	case statemachine.APhReachable:
		if obs.routeFailed {
			return statemachine.APEventEndpointsLost
		}
		if !obs.reachable {
			return statemachine.APEventRouteFail
		}
	case statemachine.APhDegraded:
		if obs.routeFailed {
			return statemachine.APEventEndpointsLost
		}
		if obs.reachable {
			return statemachine.APEventRouteOK
		}
	case statemachine.APhDown:
		if !obs.routeFailed && obs.endpointsReady {
			return statemachine.APEventEndpointsReady
		}
	}
	return ""
}

// advancePhase walks the Application machine from the current phase, one
// declared transition per observed event, until the observation is explained.
// A phase the machine does not know is an error, never overwritten.
func advancePhase(app *loopzerov1.LoopZeroApplication) (loopzerov1.ApplicationPhase, error) {
	phase := statemachine.ApplicationPhase(app.Status.Phase)
	if !knownPhases[phase] {
		return "", fmt.Errorf("application phase %q is not a LoopZeroApplication state", phase)
	}
	if applicationSM.IsTerminal(phase) {
		return app.Status.Phase, nil
	}
	obs := observe(app)
	for step := 0; step < maxPhaseSteps; step++ {
		event := nextEvent(phase, obs)
		if event == "" {
			return loopzerov1.ApplicationPhase(phase), nil
		}
		next, err := applicationSM.TryTransition(phase, event)
		if err != nil {
			return "", fmt.Errorf("application phase %q on %q: %w", phase, event, err)
		}
		phase = next
	}
	if nextEvent(phase, obs) != "" {
		return "", fmt.Errorf("application phase did not settle within %d transitions (at %q)", maxPhaseSteps, phase)
	}
	return loopzerov1.ApplicationPhase(phase), nil
}
