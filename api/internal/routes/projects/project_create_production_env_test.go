// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routes — T4.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4.
// Pins the Vercel-style "every project has production from day 1" contract
// (ADR D4) and the production-env-auto-create error path semantics.

package projects

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// fakeEnvEnsurer captures EnsureEnvironment calls so the T4.1 specs can
// assert (a) the handler invoked it with the canonical "production" env
// name on POST /api/v1/projects, and (b) the error path bubbles a 503
// instead of swallowing the failure (per ADR D4 the env auto-create is
// not optional — a project without a production env is half-provisioned).
type fakeEnvEnsurer struct {
	calls     int32
	lastT     string
	lastP     string
	lastEnv   string
	envs      []string // ordered capture of every envName ensured
	returnErr error
}

func (f *fakeEnvEnsurer) EnsureEnvironment(_ context.Context, tenantID, project, envName string) error {
	atomic.AddInt32(&f.calls, 1)
	f.lastT = tenantID
	f.lastP = project
	f.lastEnv = envName
	f.envs = append(f.envs, envName)
	return f.returnErr
}

// TestProjectCreate_AutoCreatesProductionEnv (RED 1) pins ADR D4:
// successful POST /api/v1/projects MUST invoke envEnsurer.EnsureEnvironment
// with envName="production" so the operator chain (TenantController →
// EnvironmentController) provisions the canonical M2 namespace
// `loopzero-<tenant>-<project>-production` automatically.
func TestProjectCreate_AutoCreatesProductionEnv(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	envEnsurer := &fakeEnvEnsurer{}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	h.SetEnvEnsurer(envEnsurer)

	body := `{"name":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	// #124 (qa-dogfood-cleanup-2026-06-21 T4.2 / ADR-Q5): the handler
	// MUST ensure BOTH canonical permanent envs (production + preview),
	// not just production. The legacy single-env contract was extended
	// to match the PaaS model lock (preview + production are permanent).
	if atomic.LoadInt32(&envEnsurer.calls) != 2 {
		t.Fatalf("envEnsurer.EnsureEnvironment must be called exactly twice (production + preview per ADR-Q5), got %d calls", envEnsurer.calls)
	}
	if len(envEnsurer.envs) != 2 || envEnsurer.envs[0] != "production" || envEnsurer.envs[1] != "preview" {
		t.Errorf("EnsureEnvironment ordered envs = %v, want [production preview] (canonical PermanentEnvs order)", envEnsurer.envs)
	}
	if envEnsurer.lastP != "my-api" {
		t.Errorf("EnsureEnvironment project = %q, want %q", envEnsurer.lastP, "my-api")
	}
	// TenantID from defaultUserContext — see projects_test.go helpers.
	if envEnsurer.lastT == "" {
		t.Errorf("EnsureEnvironment tenantID must propagate from user context, got empty")
	}
}

// TestProjectCreate_PropagatesEnvEnsurerFailure (RED 2) pins the error
// path: when EnsureEnvironment errors, POST returns 503 PlatformDegraded
// (matches the existing EnsureTenant failure shape — uniform UX for any
// upstream-provisioning failure on the create path). Per ADR D4 a
// half-provisioned project (row exists but env never created) is a worse
// UX than a 503 prompting retry; either the project + env both land or
// neither does.
//
// The strict EC-5 / R8 PG-tx-first/K8s-after rollback is documented as a
// known follow-up in the implementation contract — the current handler
// does NOT roll back the project row on env failure. This test pins the
// status code contract; the rollback contract is tracked separately.
func TestProjectCreate_PropagatesEnvEnsurerFailure(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	envEnsurer := &fakeEnvEnsurer{returnErr: errors.New("k8s API server unavailable")}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	h.SetEnvEnsurer(envEnsurer)

	body := `{"name":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(&envEnsurer.calls) != 1 {
		t.Fatalf("envEnsurer.EnsureEnvironment must have been called once, got %d", envEnsurer.calls)
	}
}

// TestProjectCreate_BackwardCompatNoEnsurer pins the backward-compat
// invariant: when SetEnvEnsurer is NOT called (e.g., unit tests that only
// exercise the project store path or local-only deployments that wire
// the env-creation flow elsewhere), Create behaves identically to the
// pre-T4.1 handler. The existing TestProjects_Create_HappyPath in
// projects_test.go covers this implicitly; this test asserts it
// explicitly so future refactors don't accidentally make envEnsurer
// mandatory.
func TestProjectCreate_BackwardCompatNoEnsurer(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	// envEnsurer intentionally unset.

	body := `{"name":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 (nil envEnsurer must be no-op), got %d body=%s", w.Code, w.Body.String())
	}
}
