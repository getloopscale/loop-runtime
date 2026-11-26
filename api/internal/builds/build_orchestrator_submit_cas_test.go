// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"fmt"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// build_orchestrator_submit_cas_test.go — M9 5b T-C/T-D: the two lock-free
// build status flips in SubmitBuild must use the CAS variant
// UpdateStatusIfCurrent(from=QUEUED), NOT the nil-tx read-modify-write
// UpdateStatus (ADR 2026-07-03-m9-5b-nil-tx-status-atomicity).

// countingLockerCAS records how many times WithLock is acquired, to prove
// failQueuedBuild does NOT re-acquire the advisory lock (REVIEW-BLOCKER-1: it is
// deliberately lock-free).
type countingLockerCAS struct{ calls int }

func (l *countingLockerCAS) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	l.calls++
	return fn(nil)
}

// T-C — the QUEUED→RUNNING flip after a successful workflow submit uses the
// lock-free CAS with from=QUEUED (build_orchestrator_submit.go:213).
func TestSubmitBuild_RunningFlipUsesConditionalCAS(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.ifCurrentSupersede = map[string]bool{}
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &countingLockerCAS{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"}, // submit succeeds
		EventStore: &fakeEventStoreContainer{},
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
		Apps: []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
	})
	if err != nil {
		t.Fatalf("SubmitBuild: unexpected error: %v", err)
	}

	// Exactly one CAS call: QUEUED → RUNNING (BuildStarted), NOT the RMW UpdateStatus.
	found := false
	for _, c := range bs.ifCurrentCalls {
		if c.event == string(statemachine.BPEventStarted) {
			found = true
			if c.from != statemachine.BPhQueued {
				t.Errorf("RUNNING flip from = %q, want QUEUED", c.from)
			}
			if c.tenantID != "tn-1" {
				t.Errorf("RUNNING flip tenantID = %q, want tn-1 (defense-in-depth predicate)", c.tenantID)
			}
		}
	}
	if !found {
		t.Fatalf("expected a UpdateStatusIfCurrent(QUEUED, BuildStarted) call; got ifCurrentCalls=%+v", bs.ifCurrentCalls)
	}
}

// T-C.2 — a superseded RUNNING flip (a concurrent lock-holder already advanced
// the build) returns (nil,nil) and must NOT fail SubmitBuild.
func TestSubmitBuild_RunningFlipSupersededDoesNotFail(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	// pre-create the build id the fake generates so we can mark it superseded.
	bs.ifCurrentSupersede = map[string]bool{"bld-gen-001": true}
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &countingLockerCAS{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
	})

	if _, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
		Apps: []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
	}); err != nil {
		t.Fatalf("a superseded RUNNING flip must not fail SubmitBuild; got %v", err)
	}
}

// T-D — failQueuedBuild (QUEUED→FAILED on submit failure) uses the lock-free CAS
// with from=QUEUED and does NOT re-acquire the advisory lock
// (build_orchestrator_submit.go:339, REVIEW-BLOCKER-1).
func TestFailQueuedBuild_UsesConditionalCAS_NoLock(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.ifCurrentSupersede = map[string]bool{}
	locker := &countingLockerCAS{}
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     locker,
		Workflow:   &fakeWorkflowSubmitterOrch{submitErr: fmt.Errorf("argo unreachable")}, // submit fails
		EventStore: &fakeEventStoreContainer{},
	})

	if _, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
		Apps: []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
	}); err == nil {
		t.Fatal("expected error when workflow submit fails")
	}

	// The FAILED flip must be a CAS from QUEUED.
	found := false
	for _, c := range bs.ifCurrentCalls {
		if c.event == string(statemachine.BPEventFailed) {
			found = true
			if c.from != statemachine.BPhQueued {
				t.Errorf("FAILED flip from = %q, want QUEUED", c.from)
			}
			if c.tenantID != "tn-1" {
				t.Errorf("FAILED flip tenantID = %q, want tn-1 (threaded from req.TenantID)", c.tenantID)
			}
		}
	}
	if !found {
		t.Fatalf("expected a UpdateStatusIfCurrent(QUEUED, BuildFailed) call; got ifCurrentCalls=%+v", bs.ifCurrentCalls)
	}

	// REVIEW-BLOCKER-1: failQueuedBuild is lock-free. The ONLY lock acquired is
	// the SubmitBuild reserve-slot lock — exactly one.
	if locker.calls != 1 {
		t.Errorf("WithLock acquired %d times, want 1 (failQueuedBuild must NOT re-lock)", locker.calls)
	}
}
