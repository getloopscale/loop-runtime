// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// The adapter is the whole risk of taking db.Build off the sink's surface.
// The signature change is checked by the compiler; a field silently left at its
// zero value is not, and every field below is read by the container activator:
// TenantID/ProjectID/Environment name the ArgoCD application and the
// environment lock, ID keys the SSE event stream, CreatedBy and CorrelationID
// are persisted on the release row, and CreatedAt is the deploy duration.
func TestActivationBuildFromCarriesEveryFieldActivationReads(t *testing.T) {
	createdAt := time.Date(2026, 9, 4, 10, 30, 0, 0, time.UTC)
	src := &db.Build{
		ID:            "bld-1",
		TenantID:      "tn-1",
		ProjectID:     "prj-1",
		Environment:   "production",
		CreatedBy:     "user-1",
		CorrelationID: "corr-1",
		CreatedAt:     createdAt,
		// Fields deliberately NOT on the contract — persistence detail.
		Status:     "RELEASING",
		WorkflowID: "wf-1",
	}

	got := activationBuildFrom(src)

	want := deploycontracts.ActivationBuild{
		ID:            "bld-1",
		TenantID:      "tn-1",
		ProjectID:     "prj-1",
		Environment:   "production",
		CreatedBy:     "user-1",
		CorrelationID: "corr-1",
		CreatedAt:     createdAt,
	}
	if got != want {
		t.Errorf("activationBuildFrom() = %+v, want %+v", got, want)
	}
}

// A nil build reaching the adapter must not panic on the way to the activator:
// the orchestrators fetch the row before calling, and a fetch that returned no
// row would otherwise crash the reconcile loop instead of failing the build.
func TestActivationBuildFromNilYieldsZeroValue(t *testing.T) {
	got := activationBuildFrom(nil)

	if got != (deploycontracts.ActivationBuild{}) {
		t.Errorf("activationBuildFrom(nil) = %+v, want the zero value", got)
	}
}
