// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"log/slog"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

func finalizerOver(bs *fakeBuildStoreOrch, es *fakeEventStoreContainer) buildFinalizer {
	return buildFinalizer{builds: bs, locker: &fakeLockerContainer{}, sse: es, logger: slog.Default()}
}

func (f *fakeEventStoreContainer) count(eventType string) int {
	n := 0
	for _, e := range f.events {
		if e.EventType == eventType {
			n++
		}
	}
	return n
}

func TestBuildFinalizer_FailingARunningBuildTransitionsPersistsAndEmitsOnce(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["b1"] = &db.Build{ID: "b1", Status: string(statemachine.BPhRunning)}
	es := &fakeEventStoreContainer{}

	transitioned, err := finalizerOver(bs, es).fail(context.Background(), bs.builds["b1"],
		buildFailure{reason: "boom", route: routeToFailed, stageDeployFailed: true})

	if err != nil || !transitioned {
		t.Fatalf("transitioned=%v err=%v", transitioned, err)
	}
	if bs.builds["b1"].Status != string(statemachine.BPhFailed) || bs.errorMessages["b1"] != "boom" {
		t.Errorf("status=%q message=%q", bs.builds["b1"].Status, bs.errorMessages["b1"])
	}
	if es.count("build.failed") != 1 {
		t.Errorf("build.failed emitted %d times, want 1", es.count("build.failed"))
	}
}

// A build another worker already finalized must not produce a second
// build.failed: the CLI would print the failure twice.
func TestBuildFinalizer_FailingAnAlreadyFinalBuildEmitsNothing(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["b1"] = &db.Build{ID: "b1", Status: string(statemachine.BPhSucceeded)}
	es := &fakeEventStoreContainer{}

	transitioned, err := finalizerOver(bs, es).fail(context.Background(), bs.builds["b1"],
		buildFailure{reason: "late", route: routeToFailed, stageDeployFailed: true})

	if err != nil || transitioned {
		t.Fatalf("transitioned=%v err=%v", transitioned, err)
	}
	if len(es.events) != 0 {
		t.Errorf("events = %v, want none", es.events)
	}
}

func TestBuildFinalizer_SucceedLeavesAFinalizedBuildAlone(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["b1"] = &db.Build{ID: "b1", Status: string(statemachine.BPhFailed)}

	if err := finalizerOver(bs, &fakeEventStoreContainer{}).succeed(context.Background(), bs.builds["b1"]); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, touched := bs.lastStatus["b1"]; touched {
		t.Error("a terminal build must not be transitioned")
	}
}

func TestRouteToFailed_FollowsTheMachineFromEveryFailablePhase(t *testing.T) {
	m := statemachine.NewBuildTypedMachine()
	for _, phase := range []statemachine.BuildPhase{statemachine.BPhQueued, statemachine.BPhRunning, statemachine.BPhReleasing} {
		events, err := routeToFailed(phase)
		if err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		current := phase
		for _, ev := range events {
			next, err := m.TryTransition(current, ev)
			if err != nil {
				t.Fatalf("%s: %v", phase, err)
			}
			current = next
		}
		if current != statemachine.BPhFailed {
			t.Errorf("%s routes to %s, want FAILED", phase, current)
		}
	}
	if _, err := routeToFailed(statemachine.BPhCancelling); err == nil {
		t.Error("a phase with no failure route must be refused")
	}
}
