// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"errors"
	"testing"
)

type recordedExposure struct {
	tenant, project, env, app string
	port                      int32
}

type fakeAppExposer struct {
	calls []recordedExposure
	err   error
}

func (f *fakeAppExposer) ExposeApp(_ context.Context, in AppExposure) error {
	f.calls = append(f.calls, recordedExposure{in.TenantID, in.Project, in.Environment, in.AppName, in.ServicePort})
	return f.err
}

// A promote makes the target environment reachable: every app of the promoted
// release is exposed in the target, as a deploy exposes them. On staging the
// promoted environment ran in the cluster and answered 404 on its public host
// (#124).
func TestPromote_ExposesThePromotedAppsInTheTargetEnvironment(t *testing.T) {
	fx := newPromoteTestFixture()
	exposer := &fakeAppExposer{}
	fx.orch.deps.Apps = exposer
	fx.rs.releases["rel-src-t21"].Apps = []byte(`[{"name":"api","type":"server","port":8080},{"name":"web","type":"frontend"}]`)

	if _, err := fx.orch.Promote(context.Background(), fx.req); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	want := []recordedExposure{
		{"tn-t21", "proj-t21", "production", "api", 8080},
		{"tn-t21", "proj-t21", "production", "web", 80},
	}
	if len(exposer.calls) != len(want) {
		t.Fatalf("exposed %d app(s), want %d: %+v", len(exposer.calls), len(want), exposer.calls)
	}
	for i := range want {
		if exposer.calls[i] != want[i] {
			t.Errorf("exposure %d = %+v, want %+v", i, exposer.calls[i], want[i])
		}
	}
}

// The release is already delivered when the exposure runs, so a failure there
// does not undo the promote; it is logged for the operator.
func TestPromote_AnExposureFailureDoesNotFailThePromote(t *testing.T) {
	fx := newPromoteTestFixture()
	fx.orch.deps.Apps = &fakeAppExposer{err: errors.New("management cluster unreachable")}

	if _, err := fx.orch.Promote(context.Background(), fx.req); err != nil {
		t.Fatalf("Promote failed on an exposure error: %v", err)
	}
}
